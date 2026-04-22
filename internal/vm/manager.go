package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jonnonz1/orchestrator/internal/config"
	"github.com/jonnonz1/orchestrator/internal/inject"
	"github.com/jonnonz1/orchestrator/internal/network"
)

// Manager handles the full VM lifecycle.
type Manager struct {
	mu        sync.RWMutex
	instances map[string]*VMInstance
	log       *slog.Logger
}

// NewManager creates a new VM manager and recovers state from disk.
func NewManager(log *slog.Logger) *Manager {
	m := &Manager{
		instances: make(map[string]*VMInstance),
		log:       log,
	}
	m.recoverState()
	return m
}

// Create provisions and starts a new VM.
func (m *Manager) Create(ctx context.Context, cfg VMConfig) (*VMInstance, error) {
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Reserve the name under lock *before* any slow work (rootfs copy, tap
	// setup, jailer launch). Two concurrent Create() calls with the same name
	// previously both passed the existence check — only one ended up in the
	// map, but both leaked artefacts. The sentinel entry serialises creation
	// and is either promoted to the real VMInstance on success or removed on
	// failure.
	sentinel := &VMInstance{Name: cfg.Name, State: VMStateCreating}
	m.mu.Lock()
	if _, exists := m.instances[cfg.Name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("VM %q already exists", cfg.Name)
	}
	m.instances[cfg.Name] = sentinel
	m.mu.Unlock()

	// On any error below, release the reservation so the caller can retry.
	var created bool
	defer func() {
		if !created {
			m.mu.Lock()
			if m.instances[cfg.Name] == sentinel {
				delete(m.instances, cfg.Name)
			}
			m.mu.Unlock()
		}
	}()

	// Detect host interface
	hostIface, err := detectHostInterface(ctx)
	if err != nil {
		return nil, err
	}

	// Derive network config
	netCfg := network.AllocateNetwork(cfg.Name, hostIface)

	// Derive vsock CID
	cid := cfg.CID
	if cid == 0 {
		cid = network.AutoCID(cfg.Name)
	}

	vm := &VMInstance{
		Name:       cfg.Name,
		RamMB:      cfg.RamMB,
		VCPUs:      cfg.VCPUs,
		VsockCID:   cid,
		TapDev:     netCfg.TapDev,
		TapIP:      netCfg.TapIP,
		GuestIP:    netCfg.GuestIP,
		Subnet:     netCfg.Subnet,
		HostIface:  hostIface,
		JailID:     cfg.Name,
		JailerPath: filepath.Join(JailerBase, cfg.Name),
		State:      VMStateCreating,
		RootfsPath: filepath.Join(VMDir, cfg.Name, "rootfs.ext4"),
		APISocket:  filepath.Join(JailerBase, cfg.Name, "root", "run", "firecracker.socket"),
		StateDir:   filepath.Join(VMDir, cfg.Name),
	}

	m.log.Info("creating VM",
		"name", cfg.Name,
		"ram_mb", cfg.RamMB,
		"vcpus", cfg.VCPUs,
		"guest_ip", netCfg.GuestIP,
		"vsock_cid", cid,
	)

	// Step 1: Prepare rootfs
	if err := m.prepareRootfs(ctx, vm, netCfg); err != nil {
		return nil, fmt.Errorf("prepare rootfs: %w", err)
	}

	// Step 2: Setup networking
	if err := m.setupNetwork(netCfg); err != nil {
		m.cleanupPartial(vm)
		return nil, fmt.Errorf("setup network: %w", err)
	}

	// Step 3: Setup jailer chroot
	if err := m.setupJailer(ctx, vm); err != nil {
		m.cleanupPartial(vm)
		return nil, fmt.Errorf("setup jailer: %w", err)
	}

	// Step 4: Launch firecracker via jailer
	vm.State = VMStateStarting
	if err := m.launchFirecracker(ctx, vm, netCfg); err != nil {
		m.cleanupPartial(vm)
		return nil, fmt.Errorf("launch firecracker: %w", err)
	}

	// Step 5: Save state
	vm.State = VMStateRunning
	vm.LaunchedAt = time.Now()
	if err := SaveMetadata(vm); err != nil {
		m.log.Warn("failed to save metadata", "error", err)
	}

	m.mu.Lock()
	m.instances[cfg.Name] = vm
	m.mu.Unlock()
	created = true

	m.log.Info("VM running", "name", cfg.Name, "pid", vm.PID, "guest_ip", vm.GuestIP)
	return vm, nil
}

// Get returns a VM by name.
func (m *Manager) Get(name string) (*VMInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	vm, ok := m.instances[name]
	if !ok {
		return nil, fmt.Errorf("VM %q not found", name)
	}
	return vm, nil
}

// List returns all known VMs.
func (m *Manager) List() []*VMInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*VMInstance, 0, len(m.instances))
	for _, vm := range m.instances {
		result = append(result, vm)
	}
	return result
}

// Stop kills the firecracker process but keeps state.
func (m *Manager) Stop(ctx context.Context, name string) error {
	m.mu.Lock()
	vm, ok := m.instances[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("VM %q not found", name)
	}
	m.mu.Unlock()

	if vm.State != VMStateRunning {
		return fmt.Errorf("VM %q is not running (state: %s)", name, vm.State)
	}

	m.log.Info("stopping VM", "name", name, "pid", vm.PID)
	if err := killProcess(vm.PID); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}

	vm.State = VMStateStopped
	return nil
}

// Destroy stops the VM and cleans up all resources.
func (m *Manager) Destroy(ctx context.Context, name string) error {
	m.mu.Lock()
	vm, ok := m.instances[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("VM %q not found", name)
	}
	delete(m.instances, name)
	m.mu.Unlock()

	m.log.Info("destroying VM", "name", name)

	// Kill process if running
	if vm.PID > 0 {
		killProcess(vm.PID)
	}

	netCfg := network.NetworkConfig{
		TapDev:    vm.TapDev,
		TapIP:     vm.TapIP,
		GuestIP:   vm.GuestIP,
		Subnet:    vm.Subnet,
		HostIface: vm.HostIface,
	}

	// Remove TAP device
	if err := network.TeardownTAP(vm.TapDev); err != nil {
		m.log.Warn("failed to teardown TAP", "error", err)
	}

	// Remove egress rules (no-op if none were applied)
	if err := network.TeardownEgress(vm.TapDev); err != nil {
		m.log.Warn("failed to teardown egress rules", "error", err)
	}

	// Remove iptables rules
	if err := network.TeardownNAT(netCfg); err != nil {
		m.log.Warn("failed to teardown NAT", "error", err)
	}

	// Remove jailer chroot
	if vm.JailerPath != "" {
		os.RemoveAll(vm.JailerPath)
	}

	// Remove VM state directory
	if vm.StateDir != "" {
		os.RemoveAll(vm.StateDir)
	}

	vm.State = VMStateDestroyed
	m.log.Info("VM destroyed", "name", name)
	return nil
}

// prepareRootfs copies the base rootfs and injects network config.
func (m *Manager) prepareRootfs(ctx context.Context, vm *VMInstance, netCfg network.NetworkConfig) error {
	if err := os.MkdirAll(vm.StateDir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Copy base rootfs (sparse copy)
	m.log.Info("copying rootfs", "name", vm.Name)
	cmd := exec.CommandContext(ctx, "cp", "--sparse=always", BaseRootfs, vm.RootfsPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy rootfs: %s: %w", string(out), err)
	}

	// Inject network config into rootfs
	if err := inject.InjectNetworkConfig(vm.RootfsPath, netCfg.GuestIP, netCfg.TapIP, vm.Name); err != nil {
		return fmt.Errorf("inject network config: %w", err)
	}

	return nil
}

// setupNetwork creates TAP device, iptables rules, and egress policy.
func (m *Manager) setupNetwork(netCfg network.NetworkConfig) error {
	if err := network.EnableIPForwarding(); err != nil {
		m.log.Warn("failed to enable IP forwarding (may already be enabled)", "error", err)
	}

	if err := network.SetupTAP(netCfg); err != nil {
		return fmt.Errorf("setup TAP: %w", err)
	}

	if err := network.SetupNAT(netCfg); err != nil {
		network.TeardownTAP(netCfg.TapDev)
		return fmt.Errorf("setup NAT: %w", err)
	}

	egress := network.ParseEgressAllowlist(config.Get().EgressAllowlist)
	if len(egress.Allowlist) > 0 {
		if err := network.SetupEgress(netCfg.TapDev, egress); err != nil {
			m.log.Warn("failed to apply egress allowlist", "tap", netCfg.TapDev, "error", err)
		} else {
			m.log.Info("egress allowlist applied", "tap", netCfg.TapDev, "entries", len(egress.Allowlist))
		}
	}

	return nil
}

// setupJailer creates the jailer chroot directory with kernel and rootfs.
func (m *Manager) setupJailer(ctx context.Context, vm *VMInstance) error {
	jailerRoot := filepath.Join(vm.JailerPath, "root")

	// Clean up any stale chroot
	os.RemoveAll(vm.JailerPath)

	if err := os.MkdirAll(jailerRoot, 0755); err != nil {
		return fmt.Errorf("create jailer root: %w", err)
	}

	// Copy kernel into chroot
	cmd := exec.CommandContext(ctx, "cp", KernelPath, filepath.Join(jailerRoot, "vmlinux"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy kernel: %s: %w", string(out), err)
	}

	// Copy rootfs into chroot
	cmd = exec.CommandContext(ctx, "cp", "--sparse=always", vm.RootfsPath, filepath.Join(jailerRoot, "rootfs.ext4"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy rootfs to chroot: %s: %w", string(out), err)
	}

	return nil
}

// launchFirecracker starts the firecracker process via jailer.
func (m *Manager) launchFirecracker(ctx context.Context, vm *VMInstance, netCfg network.NetworkConfig) error {
	jailerRoot := filepath.Join(vm.JailerPath, "root")

	// Write VM config
	vmConfig := map[string]interface{}{
		"boot-source": map[string]interface{}{
			"kernel_image_path": "/vmlinux",
			"boot_args":         BootArgs,
		},
		"drives": []map[string]interface{}{
			{
				"drive_id":       "rootfs",
				"path_on_host":   "/rootfs.ext4",
				"is_root_device": true,
				"is_read_only":   false,
			},
		},
		"machine-config": map[string]interface{}{
			"vcpu_count":  vm.VCPUs,
			"mem_size_mib": vm.RamMB,
		},
		"network-interfaces": []map[string]interface{}{
			{
				"iface_id":      "eth0",
				"guest_mac":     GuestMAC,
				"host_dev_name": netCfg.TapDev,
			},
		},
		"vsock": map[string]interface{}{
			"guest_cid": vm.VsockCID,
			"uds_path":  "/vsock.sock",
		},
	}

	configData, err := json.MarshalIndent(vmConfig, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal vm config: %w", err)
	}

	configPath := filepath.Join(jailerRoot, "vm-config.json")
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("write vm config: %w", err)
	}

	// Launch via jailer
	cmd := exec.CommandContext(ctx, JailerBin,
		"--id", vm.JailID,
		"--exec-file", FCBin,
		"--uid", "0",
		"--gid", "0",
		"--cgroup-version", "2",
		"--daemonize",
		"--",
		"--config-file", "/vm-config.json",
		"--api-sock", "/run/firecracker.socket",
	)

	// Capture jailer output for debugging
	jailerLog := filepath.Join(vm.StateDir, "jailer.log")
	logFile, err := os.Create(jailerLog)
	if err != nil {
		return fmt.Errorf("create jailer log: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		logFile.Close()
		logData, _ := os.ReadFile(jailerLog)
		return fmt.Errorf("jailer failed: %s: %w", string(logData), err)
	}
	logFile.Close()

	// Poll for the firecracker PID rather than sleeping blindly. Jailer
	// daemonises almost immediately but we've seen slow forks under load.
	pid, err := waitForFirecrackerPID(ctx, vm.JailID, 5*time.Second)
	if err != nil {
		return fmt.Errorf("find firecracker PID: %w", err)
	}

	vm.PID = pid
	return nil
}

// cleanupPartial cleans up a partially-created VM on failure.
func (m *Manager) cleanupPartial(vm *VMInstance) {
	m.log.Info("cleaning up partial VM", "name", vm.Name)

	if vm.TapDev != "" {
		network.TeardownTAP(vm.TapDev)
	}
	if vm.Subnet != "" && vm.HostIface != "" {
		network.TeardownNAT(network.NetworkConfig{
			TapDev:    vm.TapDev,
			Subnet:    vm.Subnet,
			HostIface: vm.HostIface,
		})
	}
	if vm.JailerPath != "" {
		os.RemoveAll(vm.JailerPath)
	}
	if vm.StateDir != "" {
		os.RemoveAll(vm.StateDir)
	}
}

// recoverState scans disk for existing VMs and rebuilds in-memory state.
func (m *Manager) recoverState() {
	entries, err := os.ReadDir(VMDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		meta, err := LoadMetadata(entry.Name())
		if err != nil {
			continue
		}

		vm := MetadataToInstance(meta)

		// Check if process is still running
		if vm.PID > 0 && processAlive(vm.PID) {
			vm.State = VMStateRunning
		} else {
			vm.State = VMStateStopped
		}

		m.instances[vm.Name] = vm
		m.log.Info("recovered VM", "name", vm.Name, "state", vm.State)
	}
}

// detectHostInterface finds the default internet-facing interface.
func detectHostInterface(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "ip", "route").Output()
	if err != nil {
		return "", fmt.Errorf("ip route: %w", err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "default") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "dev" && i+1 < len(fields) {
					return fields[i+1], nil
				}
			}
		}
	}

	return "", fmt.Errorf("could not detect default network interface")
}

// findFirecrackerPID finds the PID of a firecracker process by jail ID.
func findFirecrackerPID(jailID string) (int, error) {
	// jailID is validated by VMConfig.Validate() to [A-Za-z0-9-] so there are
	// no regex-metacharacter concerns here, but the `-F` wouldn't help because
	// we need prefix-matching on the command line.
	out, err := exec.Command("pgrep", "-f", fmt.Sprintf("firecracker.*--id %s", jailID)).Output()
	if err != nil {
		return 0, fmt.Errorf("firecracker process not found for jail %s", jailID)
	}

	lines := strings.TrimSpace(string(out))
	if lines == "" {
		return 0, fmt.Errorf("firecracker process not found for jail %s", jailID)
	}

	pid, err := strconv.Atoi(strings.Split(lines, "\n")[0])
	if err != nil {
		return 0, fmt.Errorf("parse PID: %w", err)
	}

	return pid, nil
}

// waitForFirecrackerPID polls until the jailer-spawned firecracker process
// appears in the process table, or timeout elapses.
func waitForFirecrackerPID(ctx context.Context, jailID string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		if pid, err := findFirecrackerPID(jailID); err == nil {
			return pid, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("firecracker process not found for jail %s within %s", jailID, timeout)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// killProcess sends SIGTERM, waits, then SIGKILL if needed.
func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// SIGTERM
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return nil // Process already gone
	}

	// Wait up to 5 seconds
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if !processAlive(pid) {
			return nil
		}
	}

	// SIGKILL
	proc.Signal(syscall.SIGKILL)
	return nil
}

// processAlive checks if a process is still running.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
