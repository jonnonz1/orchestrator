package mcp

import (
	"context"
	"log/slog"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/jonnonz1/orchestrator/internal/authn"
	"github.com/jonnonz1/orchestrator/internal/stream"
	"github.com/jonnonz1/orchestrator/internal/task"
	"github.com/jonnonz1/orchestrator/internal/vm"
)

// Server wraps the MCP server with orchestrator dependencies.
type Server struct {
	mcpServer  *server.MCPServer
	vmMgr      *vm.Manager
	taskStore  *task.Store
	taskRunner *task.Runner
	streamHub  *stream.Hub
	log        *slog.Logger
}

// NewServer creates a new MCP server with all tools registered.
func NewServer(vmMgr *vm.Manager, taskStore *task.Store, taskRunner *task.Runner, streamHub *stream.Hub, log *slog.Logger) *Server {
	s := &Server{
		vmMgr:      vmMgr,
		taskStore:  taskStore,
		taskRunner: taskRunner,
		streamHub:  streamHub,
		log:        log,
	}

	s.mcpServer = server.NewMCPServer(
		"orchestrator",
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	s.registerTools()
	return s
}

func (s *Server) registerTools() {
	// run_task — primary tool
	s.mcpServer.AddTool(mcplib.NewTool("run_task",
		mcplib.WithDescription("Run a Claude Code task inside an isolated Firecracker MicroVM. Creates a fresh VM, injects credentials, runs the prompt via Claude Code, and returns the result. The VM is destroyed after completion."),
		mcplib.WithString("prompt",
			mcplib.Required(),
			mcplib.Description("The prompt to give to Claude Code inside the VM"),
		),
		mcplib.WithNumber("ram_mb",
			mcplib.Description("RAM in MB (default 2048)"),
		),
		mcplib.WithNumber("vcpus",
			mcplib.Description("Number of vCPUs (default 2)"),
		),
		mcplib.WithNumber("timeout",
			mcplib.Description("Timeout in seconds (default 600)"),
		),
		mcplib.WithNumber("max_turns",
			mcplib.Description("Max Claude turns (default 50)"),
		),
		mcplib.WithString("output_dir",
			mcplib.Description("Directory in VM to collect results from (default /root/output)"),
		),
	), s.handleRunTask)

	// list_vms
	s.mcpServer.AddTool(mcplib.NewTool("list_vms",
		mcplib.WithDescription("List all Firecracker MicroVMs managed by the orchestrator"),
	), s.handleListVMs)

	// get_task_status
	s.mcpServer.AddTool(mcplib.NewTool("get_task_status",
		mcplib.WithDescription("Get the current status and output of a task"),
		mcplib.WithString("task_id",
			mcplib.Required(),
			mcplib.Description("The task ID to check"),
		),
	), s.handleGetTaskStatus)

	// exec_in_vm
	s.mcpServer.AddTool(mcplib.NewTool("exec_in_vm",
		mcplib.WithDescription("Execute a command inside a running VM via the guest agent"),
		mcplib.WithString("vm_name",
			mcplib.Required(),
			mcplib.Description("Name of the VM to execute in"),
		),
		mcplib.WithString("command",
			mcplib.Required(),
			mcplib.Description("Shell command to execute"),
		),
	), s.handleExecInVM)

	// read_vm_file
	s.mcpServer.AddTool(mcplib.NewTool("read_vm_file",
		mcplib.WithDescription("Read a file from inside a running VM"),
		mcplib.WithString("vm_name",
			mcplib.Required(),
			mcplib.Description("Name of the VM"),
		),
		mcplib.WithString("path",
			mcplib.Required(),
			mcplib.Description("Absolute path inside the VM"),
		),
	), s.handleReadVMFile)

	// destroy_vm
	s.mcpServer.AddTool(mcplib.NewTool("destroy_vm",
		mcplib.WithDescription("Stop and destroy a VM, cleaning up all resources"),
		mcplib.WithString("vm_name",
			mcplib.Required(),
			mcplib.Description("Name of the VM to destroy"),
		),
	), s.handleDestroyVM)

	// list_task_files — list result files from a completed task
	s.mcpServer.AddTool(mcplib.NewTool("list_task_files",
		mcplib.WithDescription("List all result files from a completed task. Files are downloaded from the VM before it's destroyed. Use get_task_file to retrieve the actual file contents."),
		mcplib.WithString("task_id",
			mcplib.Required(),
			mcplib.Description("The task ID to list files for"),
		),
	), s.handleListTaskFiles)

	// get_task_file — retrieve a result file (text returned as text, images as base64 image content)
	s.mcpServer.AddTool(mcplib.NewTool("get_task_file",
		mcplib.WithDescription("Get the contents of a result file from a completed task. Text/code files are returned as text. Images (png, jpg, etc.) are returned as viewable images. Other binary files are returned as base64."),
		mcplib.WithString("task_id",
			mcplib.Required(),
			mcplib.Description("The task ID"),
		),
		mcplib.WithString("filename",
			mcplib.Required(),
			mcplib.Description("The filename to retrieve (from list_task_files)"),
		),
	), s.handleGetTaskFile)
}

// ServeStdio starts the MCP server over stdio.
func (s *Server) ServeStdio() error {
	s.log.Info("MCP server starting (stdio)")
	return server.ServeStdio(s.mcpServer)
}

// ServeHTTP starts the MCP server over Streamable HTTP for network access.
// If authToken is non-empty, clients must present "Authorization: Bearer <token>".
func (s *Server) ServeHTTP(addr, authToken string) error {
	s.log.Info("MCP server starting (Streamable HTTP)", "addr", addr, "auth", authToken != "")
	httpServer := server.NewStreamableHTTPServer(s.mcpServer,
		server.WithEndpointPath("/mcp"),
		server.WithStateLess(true),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", authn.Middleware(authToken)(httpServer))
	mux.Handle("/mcp/", authn.Middleware(authToken)(httpServer))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-context.Background().Done()
		srv.Shutdown(context.Background())
	}()
	return srv.ListenAndServe()
}
