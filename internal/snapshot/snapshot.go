// Package snapshot integrates with Firecracker's pause/snapshot API to
// enable fast warm starts.
//
// Firecracker supports two snapshot artefacts:
//
//   - memory file: a page-aligned dump of guest RAM at pause time
//   - state file:  CPU registers, device state, vsock session state
//
// Restore boots a fresh Firecracker process, loads both files, and the VM
// resumes from the exact point it was paused — typically in ~150 ms.
//
// ## Warm pool
//
// 1. At server start, pre-boot N VMs from the base rootfs, wait for agent,
//    then pause+snapshot them.
// 2. On task arrival, pull a snapshot off the pool, restore (~150 ms),
//    reconnect vsock, inject context, run the agent.
// 3. Asynchronously replenish the pool.
package snapshot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jonnonz1/orchestrator/internal/config"
	"github.com/jonnonz1/orchestrator/internal/network"
	"github.com/jonnonz1/orchestrator/internal/vm"
)

// Artefact points at the two files Firecracker needs to restore a snapshot.
type Artefact struct {
	Name       string `json:"name"`
	MemoryPath string `json:"memory_path"`
	StatePath  string `json:"state_path"`
	RamMB      int    `json:"ram_mb"`
	VCPUs      int    `json:"vcpus"`
	CreatedAt  string `json:"created_at"`
}

// Manager owns the snapshot directory and warm pool.
type Manager struct {
	baseDir string
	log     *slog.Logger
	vmMgr   *vm.Manager

	poolCh   chan Artefact
	poolSize int
}

// NewManager returns a snapshot manager rooted under ORCHESTRATOR_FC_BASE/snapshots/.
func NewManager(vmMgr *vm.Manager, log *slog.Logger) *Manager {
	return &Manager{
		baseDir: filepath.Join(config.Get().FCBase, "snapshots"),
		log:     log,
		vmMgr:   vmMgr,
		poolCh:  make(chan Artefact, 64),
	}
}

// Create pauses a running VM and writes a full snapshot to disk.
//
// Steps:
//  1. Pause the VM via the Firecracker API socket.
//  2. Tell Firecracker to dump memory + state files into the jailer chroot.
//  3. Copy the artefacts out of the chroot to a stable location.
//  4. Resume or kill the source VM per the caller's intent.
func (m *Manager) Create(ctx context.Context, vmName, snapshotName string, resume bool) (Artefact, error) {
	inst, err := m.vmMgr.Get(vmName)
	if err != nil {
		return Artefact{}, fmt.Errorf("snapshot: %w", err)
	}
	if inst.State != vm.VMStateRunning {
		return Artefact{}, fmt.Errorf("snapshot: VM %q is %s, not running", vmName, inst.State)
	}

	fc := newFCClient(inst.APISocket)

	m.log.Info("pausing VM for snapshot", "vm", vmName)
	if err := fc.pauseVM(ctx); err != nil {
		return Artefact{}, fmt.Errorf("snapshot: pause %q: %w", vmName, err)
	}

	m.log.Info("creating snapshot", "vm", vmName, "name", snapshotName)
	if err := fc.createSnapshot(ctx, "/snapshot_mem", "/snapshot_state"); err != nil {
		fc.resumeVM(ctx)
		return Artefact{}, fmt.Errorf("snapshot: create: %w", err)
	}

	if resume {
		m.log.Info("resuming source VM", "vm", vmName)
		if err := fc.resumeVM(ctx); err != nil {
			m.log.Warn("failed to resume after snapshot", "vm", vmName, "error", err)
		}
	}

	snapDir := filepath.Join(m.baseDir, snapshotName)
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		return Artefact{}, fmt.Errorf("snapshot: mkdir: %w", err)
	}

	jailerRoot := filepath.Join(inst.JailerPath, "root")
	memSrc := filepath.Join(jailerRoot, "snapshot_mem")
	stateSrc := filepath.Join(jailerRoot, "snapshot_state")
	memDst := filepath.Join(snapDir, "mem")
	stateDst := filepath.Join(snapDir, "state")

	for _, cp := range [][2]string{{memSrc, memDst}, {stateSrc, stateDst}} {
		if out, err := exec.CommandContext(ctx, "cp", "--sparse=always", cp[0], cp[1]).CombinedOutput(); err != nil {
			return Artefact{}, fmt.Errorf("snapshot: copy %s → %s: %s: %w", cp[0], cp[1], string(out), err)
		}
	}

	art := Artefact{
		Name:       snapshotName,
		MemoryPath: memDst,
		StatePath:  stateDst,
		RamMB:      inst.RamMB,
		VCPUs:      inst.VCPUs,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	m.log.Info("snapshot created", "name", snapshotName, "mem", memDst, "state", stateDst)
	return art, nil
}

// Restore boots a fresh Firecracker instance from a snapshot.
//
// Steps:
//  1. Set up networking (TAP + iptables) for the new VM.
//  2. Create a jailer chroot with the snapshot artefacts.
//  3. Launch Firecracker with --no-api, loading the snapshot at boot.
//  4. The VM resumes from the exact paused state (~150 ms).
func (m *Manager) Restore(ctx context.Context, art Artefact, newVMName string) (*vm.VMInstance, error) {
	m.log.Info("restoring snapshot", "snapshot", art.Name, "new_vm", newVMName)

	hostIface, err := detectHostInterface()
	if err != nil {
		return nil, fmt.Errorf("snapshot restore: %w", err)
	}

	netCfg := network.AllocateNetwork(newVMName, hostIface)
	cid := network.AutoCID(newVMName)

	jailerPath := filepath.Join(vm.JailerBase, newVMName)
	jailerRoot := filepath.Join(jailerPath, "root")
	stateDir := filepath.Join(vm.VMDir, newVMName)

	os.RemoveAll(jailerPath)
	if err := os.MkdirAll(jailerRoot, 0755); err != nil {
		return nil, fmt.Errorf("snapshot restore: mkdir jailer: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("snapshot restore: mkdir state: %w", err)
	}

	if err := network.EnableIPForwarding(); err != nil {
		m.log.Warn("ip_forward already enabled", "error", err)
	}
	if err := network.SetupTAP(netCfg); err != nil {
		return nil, fmt.Errorf("snapshot restore: TAP: %w", err)
	}
	if err := network.SetupNAT(netCfg); err != nil {
		network.TeardownTAP(netCfg.TapDev)
		return nil, fmt.Errorf("snapshot restore: NAT: %w", err)
	}

	for _, cp := range [][2]string{
		{art.MemoryPath, filepath.Join(jailerRoot, "snapshot_mem")},
		{art.StatePath, filepath.Join(jailerRoot, "snapshot_state")},
	} {
		if out, err := exec.CommandContext(ctx, "cp", "--sparse=always", cp[0], cp[1]).CombinedOutput(); err != nil {
			network.TeardownTAP(netCfg.TapDev)
			network.TeardownNAT(netCfg)
			return nil, fmt.Errorf("snapshot restore: copy %s: %s: %w", cp[0], string(out), err)
		}
	}

	cmd := exec.CommandContext(ctx, vm.JailerBin,
		"--id", newVMName,
		"--exec-file", vm.FCBin,
		"--uid", "0",
		"--gid", "0",
		"--cgroup-version", "2",
		"--daemonize",
		"--",
		"--no-api",
		"--snapshot-path", "/snapshot_state",
		"--mem-file-path", "/snapshot_mem",
	)
	logPath := filepath.Join(stateDir, "jailer.log")
	logFile, _ := os.Create(logPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		logFile.Close()
		logData, _ := os.ReadFile(logPath)
		network.TeardownTAP(netCfg.TapDev)
		network.TeardownNAT(netCfg)
		return nil, fmt.Errorf("snapshot restore: jailer: %s: %w", string(logData), err)
	}
	logFile.Close()

	time.Sleep(500 * time.Millisecond)

	pid, err := findPID(newVMName)
	if err != nil {
		m.log.Warn("could not find restored VM PID", "error", err)
	}

	inst := &vm.VMInstance{
		Name:       newVMName,
		PID:        pid,
		RamMB:      art.RamMB,
		VCPUs:      art.VCPUs,
		VsockCID:   cid,
		TapDev:     netCfg.TapDev,
		TapIP:      netCfg.TapIP,
		GuestIP:    netCfg.GuestIP,
		Subnet:     netCfg.Subnet,
		HostIface:  hostIface,
		JailID:     newVMName,
		JailerPath: jailerPath,
		State:      vm.VMStateRunning,
		LaunchedAt: time.Now(),
		StateDir:   stateDir,
		APISocket:  filepath.Join(jailerPath, "root", "run", "firecracker.socket"),
	}

	m.log.Info("snapshot restored", "vm", newVMName, "pid", pid)
	return inst, nil
}

// List returns all snapshot artefacts discovered on disk.
func (m *Manager) List() ([]Artefact, error) {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var arts []Artefact
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		memPath := filepath.Join(m.baseDir, name, "mem")
		statePath := filepath.Join(m.baseDir, name, "state")
		if _, err := os.Stat(memPath); err != nil {
			continue
		}
		if _, err := os.Stat(statePath); err != nil {
			continue
		}
		arts = append(arts, Artefact{
			Name:       name,
			MemoryPath: memPath,
			StatePath:  statePath,
		})
	}
	return arts, nil
}

// Delete removes a snapshot from disk.
func (m *Manager) Delete(name string) error {
	return os.RemoveAll(filepath.Join(m.baseDir, name))
}

// ---- Warm pool ----

// PoolConfig configures the background warm pool.
type PoolConfig struct {
	Size  int
	RamMB int
	VCPUs int
}

// RunPool maintains a pool of pre-snapshotted VMs for fast task starts.
// Runs until ctx is cancelled. Blocks.
func (m *Manager) RunPool(ctx context.Context, cfg PoolConfig) error {
	if cfg.Size <= 0 {
		return nil
	}
	m.poolSize = cfg.Size
	m.log.Info("warm pool starting", "target_size", cfg.Size, "ram_mb", cfg.RamMB)

	for i := 0; i < cfg.Size; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := m.replenishOne(ctx, cfg, i); err != nil {
			m.log.Warn("warm pool: failed to pre-warm", "slot", i, "error", err)
		}
	}

	m.log.Info("warm pool ready", "available", len(m.poolCh))

	<-ctx.Done()
	return ctx.Err()
}

// Acquire takes a pre-warmed snapshot from the pool. Returns false if the pool
// is empty (caller should fall back to cold boot).
func (m *Manager) Acquire() (Artefact, bool) {
	select {
	case art := <-m.poolCh:
		return art, true
	default:
		return Artefact{}, false
	}
}

// Replenish creates one fresh snapshot and adds it to the pool.
func (m *Manager) Replenish(ctx context.Context, cfg PoolConfig) {
	slot := time.Now().UnixNano() % 10000
	if err := m.replenishOne(ctx, cfg, int(slot)); err != nil {
		m.log.Warn("warm pool: replenish failed", "error", err)
	}
}

func (m *Manager) replenishOne(ctx context.Context, cfg PoolConfig, slot int) error {
	vmName := fmt.Sprintf("pool-%d", slot)
	snapName := fmt.Sprintf("pool-snap-%d", slot)

	_, err := m.vmMgr.Create(ctx, vm.VMConfig{
		Name:  vmName,
		RamMB: cfg.RamMB,
		VCPUs: cfg.VCPUs,
	})
	if err != nil {
		return fmt.Errorf("create pool VM: %w", err)
	}

	time.Sleep(3 * time.Second)

	art, err := m.Create(ctx, vmName, snapName, false)
	if err != nil {
		m.vmMgr.Destroy(ctx, vmName)
		return fmt.Errorf("snapshot pool VM: %w", err)
	}

	m.vmMgr.Destroy(ctx, vmName)

	select {
	case m.poolCh <- art:
	default:
		m.log.Warn("warm pool channel full, discarding snapshot", "name", snapName)
		m.Delete(snapName)
	}
	return nil
}

func detectHostInterface() (string, error) {
	out, err := exec.Command("ip", "route").Output()
	if err != nil {
		return "", fmt.Errorf("ip route: %w", err)
	}
	for _, line := range splitLines(string(out)) {
		fields := splitFields(line)
		for i, f := range fields {
			if f == "dev" && i+1 < len(fields) {
				return fields[i+1], nil
			}
		}
	}
	return "", fmt.Errorf("could not detect default interface")
}

func findPID(jailID string) (int, error) {
	out, err := exec.Command("pgrep", "-f", fmt.Sprintf("firecracker.*--id %s", jailID)).Output()
	if err != nil {
		return 0, err
	}
	var pid int
	fmt.Sscanf(splitLines(string(out))[0], "%d", &pid)
	return pid, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFields(s string) []string {
	var fields []string
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\t' {
			if start >= 0 {
				fields = append(fields, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, s[start:])
	}
	return fields
}
