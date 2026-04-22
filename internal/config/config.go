// Package config centralizes runtime-configurable paths and defaults.
//
// All paths may be overridden by environment variables so the orchestrator
// can run in non-standard layouts (containers, test harnesses, alternative
// Firecracker installations).
package config

import (
	"os"
	"path/filepath"
)

// EnvPrefix is the prefix for all Orchestrator environment variables.
const EnvPrefix = "ORCHESTRATOR_"

// Paths holds runtime-configurable filesystem locations.
type Paths struct {
	// FCBase is where Firecracker binaries, kernel, rootfs, and VM state live.
	FCBase string

	// FCBin is the Firecracker binary path.
	FCBin string

	// JailerBin is the jailer binary path.
	JailerBin string

	// KernelPath is the guest kernel image.
	KernelPath string

	// BaseRootfs is the template rootfs that gets copied for each VM.
	BaseRootfs string

	// VMDir is where per-VM metadata + cloned rootfs are kept.
	VMDir string

	// JailerBase is the jailer chroot root. Firecracker's jailer hardcodes
	// "/srv/jailer/firecracker" in some tooling; we honor it by default but
	// make it overridable.
	JailerBase string

	// ResultsDir is where task result files are downloaded on the host.
	ResultsDir string

	// AuditLogPath is where structured audit log entries are appended.
	// Empty string disables audit logging.
	AuditLogPath string

	// EgressAllowlist is a comma-separated list of IPs, CIDRs, or hostnames.
	// Empty = unrestricted (default). When set, VMs can only reach these
	// destinations + DNS + api.anthropic.com.
	EgressAllowlist string
}

// Default returns the default paths (matches pre-config constants).
func Default() Paths {
	base := envOr("FC_BASE", "/opt/firecracker")
	return Paths{
		FCBase:       base,
		FCBin:        envOr("FC_BIN", filepath.Join(base, "firecracker")),
		JailerBin:    envOr("JAILER_BIN", filepath.Join(base, "jailer")),
		KernelPath:   envOr("KERNEL", filepath.Join(base, "kernels", "vmlinux")),
		BaseRootfs:   envOr("BASE_ROOTFS", filepath.Join(base, "rootfs", "base-rootfs.ext4")),
		VMDir:        envOr("VM_DIR", filepath.Join(base, "vms")),
		JailerBase:   envOr("JAILER_BASE", "/srv/jailer/firecracker"),
		ResultsDir:   envOr("RESULTS_DIR", filepath.Join(base, "results")),
		AuditLogPath:    os.Getenv(EnvPrefix + "AUDIT_LOG"),
		EgressAllowlist: os.Getenv(EnvPrefix + "EGRESS_ALLOWLIST"),
	}
}

// envOr returns the ORCHESTRATOR_<key> env var or the default.
func envOr(key, def string) string {
	if v := os.Getenv(EnvPrefix + key); v != "" {
		return v
	}
	return def
}

// Server holds server/network configuration.
type Server struct {
	// Addr is the bind address for the REST API + dashboard.
	// Defaults to 127.0.0.1:8080 (loopback-only for safety).
	Addr string

	// MCPAddr is the bind address for the MCP Streamable-HTTP server.
	// Defaults to 127.0.0.1:8081 (loopback-only for safety).
	MCPAddr string

	// AuthToken, if set, requires all HTTP clients to present it as a bearer
	// token. Required when binding a non-loopback address. Empty disables auth
	// on loopback bindings.
	AuthToken string

	// CORSOrigins is a comma-separated list of allowed CORS origins. Empty
	// means "same-origin only" (no Access-Control-Allow-Origin header is set
	// and the default CORS middleware is not installed). Use "*" to allow any
	// origin (credentials will not be forwarded on that path).
	CORSOrigins string

	// WebhookURL, if set, receives task lifecycle events as HMAC-signed POSTs.
	WebhookURL string

	// WebhookSecret is the HMAC-SHA256 secret for webhook signatures.
	WebhookSecret string
}

// DefaultServer returns default server config with loopback binding.
func DefaultServer() Server {
	return Server{
		Addr:          envOr("ADDR", "127.0.0.1:8080"),
		MCPAddr:       envOr("MCP_ADDR", "127.0.0.1:8081"),
		AuthToken:     os.Getenv(EnvPrefix + "AUTH_TOKEN"),
		CORSOrigins:   os.Getenv(EnvPrefix + "CORS_ORIGINS"),
		WebhookURL:    os.Getenv(EnvPrefix + "WEBHOOK_URL"),
		WebhookSecret: os.Getenv(EnvPrefix + "WEBHOOK_SECRET"),
	}
}

// Package-level cached defaults for convenience. Call Reload() if env vars
// are mutated at runtime (rare — mostly for tests).
var (
	paths  = Default()
	server = DefaultServer()
)

// Get returns the current cached Paths.
func Get() Paths { return paths }

// GetServer returns the current cached Server config.
func GetServer() Server { return server }

// Reload re-reads environment variables. Intended for tests.
func Reload() {
	paths = Default()
	server = DefaultServer()
}
