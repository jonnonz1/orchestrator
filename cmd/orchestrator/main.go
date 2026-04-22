// Orchestrator — MicroVM orchestrator for AI agents.
//
// Build with `make build`. The resulting binary is named `orchestrator` but accepts
// either name at runtime.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jonnonz1/orchestrator/internal/agent"
	"github.com/jonnonz1/orchestrator/internal/api"
	"github.com/jonnonz1/orchestrator/internal/authn"
	"github.com/jonnonz1/orchestrator/internal/config"
	"github.com/jonnonz1/orchestrator/internal/events"
	mcpsrv "github.com/jonnonz1/orchestrator/internal/mcp"
	"github.com/jonnonz1/orchestrator/internal/snapshot"
	"github.com/jonnonz1/orchestrator/internal/stream"
	"github.com/jonnonz1/orchestrator/internal/task"
	"github.com/jonnonz1/orchestrator/internal/vm"
)

//go:embed all:web-dist
var webDistEmbed embed.FS

// Version is stamped in at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "vm":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator vm <create|list|get|stop|destroy>")
			os.Exit(1)
		}
		handleVM(os.Args[2:], log)
	case "task":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator task <run>")
			os.Exit(1)
		}
		handleTask(os.Args[2:], log)
	case "mcp":
		handleMCP(log)
	case "mcp-serve":
		handleMCPServe(os.Args[2:], log)
	case "serve":
		handleServe(os.Args[2:], log)
	case "snapshot":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator snapshot <create|restore|list|delete>")
			os.Exit(1)
		}
		handleSnapshot(os.Args[2:], log)
	case "version", "-v", "--version":
		fmt.Println("orchestrator", Version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `orchestrator — MicroVM orchestrator for AI agents

Usage: orchestrator <command>

Commands:
  vm        Manage MicroVMs (create, list, get, stop, destroy)
  task      Run a task end-to-end (create VM → run agent → collect → destroy)
  snapshot  Manage VM snapshots (create, restore, list, delete)
  serve     Start the REST API + embedded web dashboard
  mcp       Start the MCP server over stdio (for local Claude Code)
  mcp-serve Start the MCP server over HTTP (for LAN access)
  version   Print version and exit
  help      Show this message

Environment:
  ORCHESTRATOR_FC_BASE        Firecracker layout root (default /opt/firecracker)
  ORCHESTRATOR_JAILER_BASE    Jailer chroot base (default /srv/jailer/firecracker)
  ORCHESTRATOR_RESULTS_DIR    Task results directory
  ORCHESTRATOR_ADDR           REST API bind address (default 127.0.0.1:8080)
  ORCHESTRATOR_MCP_ADDR       MCP server bind address (default 127.0.0.1:8081)
  ORCHESTRATOR_AUTH_TOKEN     Bearer token required for non-loopback HTTP
  ORCHESTRATOR_AUDIT_LOG      Path to JSON-lines audit log (default: disabled)
  ANTHROPIC_API_KEY           If set, used instead of ~/.claude/.credentials.json
  ORCHESTRATOR_WEBHOOK_URL    HTTP URL receiving task lifecycle events
  ORCHESTRATOR_WEBHOOK_SECRET HMAC-SHA256 secret for webhook signatures`)
}

func handleVM(args []string, log *slog.Logger) {
	mgr := vm.NewManager(log)
	ctx, cancel := signalContext()
	defer cancel()

	switch args[0] {
	case "create":
		cfg := vm.VMConfig{}
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--name":
				cfg.Name = flagValue(args, &i, "--name")
			case "--ram":
				fmt.Sscanf(flagValue(args, &i, "--ram"), "%d", &cfg.RamMB)
			case "--vcpus":
				fmt.Sscanf(flagValue(args, &i, "--vcpus"), "%d", &cfg.VCPUs)
			}
		}

		if cfg.Name == "" {
			fmt.Fprintln(os.Stderr, "ERROR: --name is required")
			os.Exit(1)
		}

		requireRoot()

		instance, err := mgr.Create(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}

		printVM(instance)

	case "list":
		instances := mgr.List()
		if len(instances) == 0 {
			fmt.Println("No VMs found")
			return
		}
		fmt.Printf("%-20s %-10s %-8s %-6s %-18s %-8s\n", "NAME", "STATE", "RAM(MB)", "VCPUS", "GUEST IP", "PID")
		for _, inst := range instances {
			fmt.Printf("%-20s %-10s %-8d %-6d %-18s %-8d\n",
				inst.Name, inst.State, inst.RamMB, inst.VCPUs, inst.GuestIP, inst.PID)
		}

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator vm get <name>")
			os.Exit(1)
		}
		name := args[1]
		for i := 1; i < len(args); i++ {
			if args[i] == "--name" {
				name = flagValue(args, &i, "--name")
			}
		}

		instance, err := mgr.Get(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		printVM(instance)

	case "stop":
		name := parseName(args[1:])
		requireRoot()
		if err := mgr.Stop(ctx, name); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("VM %q stopped\n", name)

	case "destroy":
		name := parseName(args[1:])
		requireRoot()
		if err := mgr.Destroy(ctx, name); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("VM %q destroyed\n", name)

	default:
		fmt.Fprintf(os.Stderr, "Unknown vm command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: orchestrator vm <create|list|get|stop|destroy>")
		os.Exit(1)
	}
}

func requireRoot() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "ERROR: Must be run as root (use sudo)")
		os.Exit(1)
	}
}

func parseName(args []string) string {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "ERROR: VM name required")
		os.Exit(1)
	}
	name := args[0]
	for i := 0; i < len(args); i++ {
		if args[i] == "--name" && i+1 < len(args) {
			name = args[i+1]
		}
	}
	return name
}

// flagValue advances *i past the flag's value and returns it. Exits with a
// clear error if the value is missing instead of panicking on an out-of-bounds
// index (the previous pattern did `i++; x = args[i]` which crashed on
// trailing flags without a value).
func flagValue(args []string, i *int, flag string) string {
	if *i+1 >= len(args) {
		fmt.Fprintf(os.Stderr, "ERROR: %s requires a value\n", flag)
		os.Exit(1)
	}
	*i++
	return args[*i]
}

// signalContext returns a context that is cancelled on SIGINT or SIGTERM so
// long-running operations (task run, VM destroy) can clean up gracefully.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func printVM(inst *vm.VMInstance) {
	data, _ := json.MarshalIndent(inst, "", "  ")
	fmt.Println(string(data))
}

func handleTask(args []string, log *slog.Logger) {
	requireRoot()

	switch args[0] {
	case "run":
		t := &task.Task{AutoDestroy: true}
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--prompt", "-p":
				t.Prompt = flagValue(args, &i, args[i])
			case "--ram":
				fmt.Sscanf(flagValue(args, &i, "--ram"), "%d", &t.RamMB)
			case "--vcpus":
				fmt.Sscanf(flagValue(args, &i, "--vcpus"), "%d", &t.VCPUs)
			case "--timeout":
				fmt.Sscanf(flagValue(args, &i, "--timeout"), "%d", &t.Timeout)
			case "--max-turns":
				fmt.Sscanf(flagValue(args, &i, "--max-turns"), "%d", &t.MaxTurns)
			case "--runtime":
				t.Runtime = flagValue(args, &i, "--runtime")
			case "--no-destroy":
				t.AutoDestroy = false
			}
		}

		if t.Prompt == "" {
			fmt.Fprintln(os.Stderr, "ERROR: --prompt is required")
			os.Exit(1)
		}

		ctx, cancel := signalContext()
		defer cancel()
		mgr := vm.NewManager(log)
		store := task.NewStore()
		runner := task.NewRunner(mgr, store, log)

		onEvent := func(taskID string, event agent.StreamEvent) {
			switch event.Type {
			case agent.StreamEventStdout:
				fmt.Println(event.Data)
			case agent.StreamEventStderr:
				fmt.Fprintf(os.Stderr, "%s\n", event.Data)
			case agent.StreamEventExit:
				fmt.Fprintf(os.Stderr, "\n[exit code: %s]\n", event.Data)
			}
		}

		if err := runner.Run(ctx, t, onEvent); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "\n=== Task Complete ===\n")
		fmt.Fprintf(os.Stderr, "ID:     %s\n", t.ID)
		fmt.Fprintf(os.Stderr, "Status: %s\n", t.Status)
		if t.ExitCode != nil {
			fmt.Fprintf(os.Stderr, "Exit:   %d\n", *t.ExitCode)
		}
		if t.CostUSD > 0 {
			fmt.Fprintf(os.Stderr, "Cost:   $%.4f\n", t.CostUSD)
		}
		if len(t.ResultFiles) > 0 {
			fmt.Fprintf(os.Stderr, "Files:  %v\n", t.ResultFiles)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown task command: %s\n", args[0])
		os.Exit(1)
	}
}

func handleServe(args []string, log *slog.Logger) {
	requireRoot()

	srvCfg := config.GetServer()
	addr := srvCfg.Addr
	authToken := srvCfg.AuthToken
	insecure := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 < len(args) {
				addr = args[i+1]
				i++
			}
		case "--port":
			if i+1 < len(args) {
				var port int
				fmt.Sscanf(args[i+1], "%d", &port)
				addr = fmt.Sprintf("127.0.0.1:%d", port)
				i++
			}
		case "--auth-token":
			if i+1 < len(args) {
				authToken = args[i+1]
				i++
			}
		case "--insecure":
			insecure = true
		}
	}

	token, authEnabled, err := resolveAuthForAddr(addr, authToken, insecure, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	mgr := vm.NewManager(log)
	store := task.NewStore()
	hub := stream.NewHub()
	runner := task.NewRunner(mgr, store, log)

	// Wire event sinks (webhook + audit log) from env vars.
	runner.Events = buildEventSinks(log)

	webFS, _ := fs.Sub(webDistEmbed, "web-dist")
	api.WebDist = webFS

	srv := api.NewServer(mgr, store, runner, hub, log)
	if authEnabled {
		srv.SetAuthToken(token)
	}
	if origins := parseOrigins(srvCfg.CORSOrigins); len(origins) > 0 {
		srv.SetCORSOrigins(origins)
	}

	if err := srv.ListenAndServe(addr); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

// parseOrigins splits a comma-separated list of CORS origins and trims whitespace.
func parseOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func handleMCPServe(args []string, log *slog.Logger) {
	srvCfg := config.GetServer()
	addr := srvCfg.MCPAddr
	authToken := srvCfg.AuthToken
	insecure := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 < len(args) {
				addr = args[i+1]
				i++
			}
		case "--auth-token":
			if i+1 < len(args) {
				authToken = args[i+1]
				i++
			}
		case "--insecure":
			insecure = true
		}
	}

	token, authEnabled, err := resolveAuthForAddr(addr, authToken, insecure, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	mgr := vm.NewManager(log)
	store := task.NewStore()
	hub := stream.NewHub()
	runner := task.NewRunner(mgr, store, log)

	srv := mcpsrv.NewServer(mgr, store, runner, hub, log)

	fmt.Fprintf(os.Stderr, "MCP server (Streamable HTTP) listening on %s\n", addr)
	fmt.Fprintf(os.Stderr, "Endpoint: http://%s/mcp  (auth: %v)\n", addr, authEnabled)
	if err := srv.ServeHTTP(addr, token); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func handleMCP(log *slog.Logger) {
	// stdio transport — redirect slog to stderr so it doesn't interfere with JSON-RPC on stdout.
	log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	mgr := vm.NewManager(log)
	store := task.NewStore()
	hub := stream.NewHub()
	runner := task.NewRunner(mgr, store, log)

	srv := mcpsrv.NewServer(mgr, store, runner, hub, log)

	if err := srv.ServeStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func handleSnapshot(args []string, log *slog.Logger) {
	requireRoot()
	mgr := vm.NewManager(log)
	snapMgr := snapshot.NewManager(mgr, log)
	ctx, cancel := signalContext()
	defer cancel()

	switch args[0] {
	case "create":
		var vmName, snapName string
		resume := false
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--vm":
				vmName = flagValue(args, &i, "--vm")
			case "--name":
				snapName = flagValue(args, &i, "--name")
			case "--resume":
				resume = true
			}
		}
		if vmName == "" || snapName == "" {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator snapshot create --vm <vm> --name <snapshot> [--resume]")
			os.Exit(1)
		}
		art, err := snapMgr.Create(ctx, vmName, snapName, resume)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(art, "", "  ")
		fmt.Println(string(data))

	case "restore":
		var snapName, newVM string
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--name":
				snapName = flagValue(args, &i, "--name")
			case "--vm":
				newVM = flagValue(args, &i, "--vm")
			}
		}
		if snapName == "" || newVM == "" {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator snapshot restore --name <snapshot> --vm <new-vm-name>")
			os.Exit(1)
		}
		arts, err := snapMgr.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		var found *snapshot.Artefact
		for _, a := range arts {
			if a.Name == snapName {
				found = &a
				break
			}
		}
		if found == nil {
			fmt.Fprintf(os.Stderr, "ERROR: snapshot %q not found\n", snapName)
			os.Exit(1)
		}
		inst, err := snapMgr.Restore(ctx, *found, newVM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		printVM(inst)

	case "list":
		arts, err := snapMgr.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		if len(arts) == 0 {
			fmt.Println("No snapshots found")
			return
		}
		fmt.Printf("%-30s %-50s %-50s\n", "NAME", "MEMORY", "STATE")
		for _, a := range arts {
			fmt.Printf("%-30s %-50s %-50s\n", a.Name, a.MemoryPath, a.StatePath)
		}

	case "delete":
		var snapName string
		for i := 1; i < len(args); i++ {
			if args[i] == "--name" && i+1 < len(args) {
				snapName = args[i+1]
			}
		}
		if snapName == "" {
			fmt.Fprintln(os.Stderr, "Usage: orchestrator snapshot delete --name <snapshot>")
			os.Exit(1)
		}
		if err := snapMgr.Delete(snapName); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Snapshot %q deleted\n", snapName)

	default:
		fmt.Fprintf(os.Stderr, "Unknown snapshot command: %s\n", args[0])
		os.Exit(1)
	}
}

// resolveAuthForAddr applies the auth policy:
//   - loopback: auth optional
//   - non-loopback: auth required (token generated if not provided, unless --insecure)
func resolveAuthForAddr(addr, providedToken string, insecure bool, log *slog.Logger) (string, bool, error) {
	if insecure {
		log.Warn("AUTH DISABLED via --insecure flag — do not expose this server publicly")
		return "", false, nil
	}
	return authn.PolicyFor(addr, providedToken, log)
}

// buildEventSinks wires webhook + audit log sinks from env vars.
// Returns nil if no sinks are configured.
func buildEventSinks(log *slog.Logger) events.Sink {
	srvCfg := config.GetServer()
	paths := config.Get()

	var sinks events.Multi

	if webhook := events.NewWebhookSender(srvCfg.WebhookURL, srvCfg.WebhookSecret, log); webhook != nil {
		log.Info("webhook events enabled", "url", srvCfg.WebhookURL)
		sinks = append(sinks, webhook)
	}

	if paths.AuditLogPath != "" {
		audit, err := events.NewAuditLogger(paths.AuditLogPath, log)
		if err != nil {
			log.Warn("audit log init failed", "path", paths.AuditLogPath, "error", err)
		} else if audit != nil {
			log.Info("audit log enabled", "path", paths.AuditLogPath)
			sinks = append(sinks, audit)
		}
	}

	if len(sinks) == 0 {
		return nil
	}
	return sinks
}
