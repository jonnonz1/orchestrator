package vm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Metadata is the JSON structure persisted to disk for each VM.
// It matches the existing metadata.json schema from the shell scripts.
type Metadata struct {
	Name       string `json:"name"`
	PID        int    `json:"pid"`
	RamMB      int    `json:"ram_mb"`
	VCPUs      int    `json:"vcpus"`
	VsockCID   uint32 `json:"vsock_cid"`
	TapDev     string `json:"tap_dev"`
	TapIP      string `json:"tap_ip"`
	GuestIP    string `json:"guest_ip"`
	Subnet     string `json:"subnet"`
	HostIface  string `json:"host_iface"`
	JailID     string `json:"jail_id"`
	JailerBase string `json:"jailer_base"`
	LaunchedAt string `json:"launched_at"`
}

// SaveMetadata writes the VM metadata to disk.
func SaveMetadata(vm *VMInstance) error {
	meta := Metadata{
		Name:       vm.Name,
		PID:        vm.PID,
		RamMB:      vm.RamMB,
		VCPUs:      vm.VCPUs,
		VsockCID:   vm.VsockCID,
		TapDev:     vm.TapDev,
		TapIP:      vm.TapIP,
		GuestIP:    vm.GuestIP,
		Subnet:     vm.Subnet,
		HostIface:  vm.HostIface,
		JailID:     vm.JailID,
		JailerBase: vm.JailerPath,
		LaunchedAt: vm.LaunchedAt.Format("2006-01-02T15:04:05-07:00"),
	}

	data, err := json.MarshalIndent(meta, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	metaPath := filepath.Join(vm.StateDir, "metadata.json")
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	pidPath := filepath.Join(vm.StateDir, "pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", vm.PID)), 0644); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}

	return nil
}

// LoadMetadata reads VM metadata from disk.
func LoadMetadata(name string) (*Metadata, error) {
	metaPath := filepath.Join(VMDir, name, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	return &meta, nil
}

// MetadataToInstance converts persisted metadata to a VMInstance.
func MetadataToInstance(meta *Metadata) *VMInstance {
	return &VMInstance{
		Name:       meta.Name,
		PID:        meta.PID,
		RamMB:      meta.RamMB,
		VCPUs:      meta.VCPUs,
		VsockCID:   meta.VsockCID,
		TapDev:     meta.TapDev,
		TapIP:      meta.TapIP,
		GuestIP:    meta.GuestIP,
		Subnet:     meta.Subnet,
		HostIface:  meta.HostIface,
		JailID:     meta.JailID,
		JailerPath: meta.JailerBase,
		RootfsPath: filepath.Join(VMDir, meta.Name, "rootfs.ext4"),
		APISocket:  filepath.Join(meta.JailerBase, "root", "run", "firecracker.socket"),
		StateDir:   filepath.Join(VMDir, meta.Name),
	}
}
