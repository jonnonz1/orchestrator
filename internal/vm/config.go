package vm

import (
	"fmt"
	"time"

	"github.com/jonnonz1/orchestrator/internal/config"
)

// Runtime-configurable paths. These read from ORCHESTRATOR_* env vars via
// internal/config at package init. They are variables (not constants) so
// tests and embedders can override them.
var (
	FCBase     = config.Get().FCBase
	FCBin      = config.Get().FCBin
	JailerBin  = config.Get().JailerBin
	KernelPath = config.Get().KernelPath
	BaseRootfs = config.Get().BaseRootfs
	VMDir      = config.Get().VMDir
	JailerBase = config.Get().JailerBase
)

const (
	GuestMAC = "06:00:AC:10:00:02"
	BootArgs = "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init"
)

// ReloadPaths re-reads path config from environment. Used by tests.
func ReloadPaths() {
	config.Reload()
	p := config.Get()
	FCBase = p.FCBase
	FCBin = p.FCBin
	JailerBin = p.JailerBin
	KernelPath = p.KernelPath
	BaseRootfs = p.BaseRootfs
	VMDir = p.VMDir
	JailerBase = p.JailerBase
}

// VMState represents the lifecycle state of a VM.
type VMState string

const (
	VMStateCreating  VMState = "creating"
	VMStateStarting  VMState = "starting"
	VMStateRunning   VMState = "running"
	VMStateStopped   VMState = "stopped"
	VMStateDestroyed VMState = "destroyed"
	VMStateError     VMState = "error"
)

// VMConfig is the user-provided configuration for creating a VM.
type VMConfig struct {
	Name  string `json:"name"`
	RamMB int    `json:"ram_mb"`
	VCPUs int    `json:"vcpus"`
	CID   uint32 `json:"cid,omitempty"` // 0 = auto-assign
}

// VMInstance represents a running or stopped VM with all computed state.
type VMInstance struct {
	Name       string    `json:"name"`
	PID        int       `json:"pid"`
	RamMB      int       `json:"ram_mb"`
	VCPUs      int       `json:"vcpus"`
	VsockCID   uint32    `json:"vsock_cid"`
	TapDev     string    `json:"tap_dev"`
	TapIP      string    `json:"tap_ip"`
	GuestIP    string    `json:"guest_ip"`
	Subnet     string    `json:"subnet"`
	HostIface  string    `json:"host_iface"`
	JailID     string    `json:"jail_id"`
	JailerPath string    `json:"jailer_base"`
	State      VMState   `json:"state"`
	LaunchedAt time.Time `json:"launched_at"`

	// Derived paths (not persisted in JSON output)
	RootfsPath string `json:"-"`
	APISocket  string `json:"-"`
	StateDir   string `json:"-"`
}

// Defaults applies default values to a config.
func (c *VMConfig) Defaults() {
	if c.RamMB == 0 {
		c.RamMB = 512
	}
	if c.VCPUs == 0 {
		c.VCPUs = 1
	}
}

// Validate checks that the config is valid.
func (c *VMConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	for i, ch := range c.Name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || (ch == '-' && i > 0)) {
			return fmt.Errorf("name must be alphanumeric with hyphens (cannot start with hyphen)")
		}
	}
	if c.RamMB < 128 || c.RamMB > 32768 {
		return fmt.Errorf("ram_mb must be between 128 and 32768")
	}
	if c.VCPUs < 1 || c.VCPUs > 32 {
		return fmt.Errorf("vcpus must be between 1 and 32")
	}
	return nil
}
