package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jonnonz1/orchestrator/internal/agent"
	"github.com/jonnonz1/orchestrator/internal/config"
	"github.com/jonnonz1/orchestrator/internal/events"
	"github.com/jonnonz1/orchestrator/internal/metrics"
	"github.com/jonnonz1/orchestrator/internal/runtime"
	"github.com/jonnonz1/orchestrator/internal/vm"
	"github.com/jonnonz1/orchestrator/internal/vsock"
)

// envVarNameRE matches a POSIX environment variable name. Keys from user
// input are validated against this before being injected into the guest's
// /etc/profile.d/claude.sh so that an attacker with task-creation privileges
// cannot inject arbitrary shell (the guest is disposable so the impact is
// bounded, but this closes the foot-gun).
var envVarNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ResultsDir is where task results are downloaded to on the host.
// Overridable via ORCHESTRATOR_RESULTS_DIR.
var ResultsDir = config.Get().ResultsDir

// Runner orchestrates the full task lifecycle:
// create VM -> wait for agent -> inject context -> run Claude Code -> collect results -> destroy VM
type Runner struct {
	vmMgr *vm.Manager
	store *Store
	log   *slog.Logger

	// Host-side credentials path
	CredentialsPath string

	// Runtimes is the registry used to resolve task.Runtime. Defaults to
	// runtime.Default (claude + shell). Embedders can override with a custom
	// registry to add more runtimes.
	Runtimes *runtime.Registry

	// Metrics is optional; if set, the runner reports task + boot metrics.
	Metrics *metrics.Collector

	// Events is optional; if set, task lifecycle events are dispatched to it.
	// Combine audit.Logger + webhook.Sender with events.Multi.
	Events events.Sink
}

// OnEvent is the per-invocation streaming callback signature.
type OnEvent func(taskID string, event agent.StreamEvent)

// NewRunner creates a new task runner.
func NewRunner(vmMgr *vm.Manager, store *Store, log *slog.Logger) *Runner {
	homeDir := realHomeDir()
	return &Runner{
		vmMgr:           vmMgr,
		store:           store,
		log:             log,
		CredentialsPath: filepath.Join(homeDir, ".claude", ".credentials.json"),
		Runtimes:        runtime.Default,
	}
}

// realHomeDir returns the home directory of the actual user (not root under sudo).
// Uses os/user so non-standard home layouts (/Users, /var/lib/…) are handled.
func realHomeDir() string {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.HomeDir
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// Run executes a task end-to-end. It blocks until the task completes.
// onEvent is invoked for every stream event (stdout/stderr/exit); pass nil to
// ignore streaming. The callback is per-invocation so concurrent tasks do not
// race on a shared field.
func (r *Runner) Run(ctx context.Context, t *Task, onEvent OnEvent) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	t.Defaults()
	t.Status = StatusPending
	t.CreatedAt = time.Now()
	r.store.Put(t)

	// Generate VM name from task ID
	if t.VMName == "" {
		t.VMName = "task-" + t.ID
	}

	r.log.Info("starting task", "id", t.ID, "vm", t.VMName, "prompt_len", len(t.Prompt))
	if r.Metrics != nil {
		r.Metrics.ObserveTaskStarted()
	}
	r.emit(events.Event{
		ID:        t.ID,
		Type:      events.TypeTaskStarted,
		Timestamp: time.Now(),
		TaskID:    t.ID,
		VMName:    t.VMName,
		Data: map[string]interface{}{
			"runtime":    t.Runtime,
			"ram_mb":     t.RamMB,
			"vcpus":      t.VCPUs,
			"prompt_len": len(t.Prompt),
		},
	})

	// Apply task timeout to context
	if t.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t.Timeout)*time.Second)
		defer cancel()
	}

	// Step 1: Create VM
	now := time.Now()
	t.Status = StatusRunning
	t.StartedAt = &now

	bootStart := time.Now()
	instance, err := r.vmMgr.Create(ctx, vm.VMConfig{
		Name:  t.VMName,
		RamMB: t.RamMB,
		VCPUs: t.VCPUs,
	})
	if err != nil {
		return r.fail(t, fmt.Errorf("create VM: %w", err))
	}
	if r.Metrics != nil {
		r.Metrics.ObserveVMBoot(time.Since(bootStart))
	}

	// Ensure cleanup. Use a fresh context for destruction so that a cancelled
	// task context (timeout, ctrl-c) does not prevent teardown of the VM and
	// its network/iptables state.
	if t.AutoDestroy {
		defer func() {
			destroyCtx, destroyCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer destroyCancel()
			r.log.Info("destroying task VM", "id", t.ID, "vm", t.VMName)
			r.vmMgr.Destroy(destroyCtx, t.VMName)
		}()
	}

	// Step 2: Wait for agent
	r.log.Info("waiting for agent", "id", t.ID)
	if err := r.waitForAgent(instance.JailID, 30*time.Second); err != nil {
		return r.fail(t, fmt.Errorf("agent not ready: %w", err))
	}
	r.log.Info("agent ready", "id", t.ID)

	// Step 3: Inject context via vsock
	if err := r.injectContext(instance.JailID, t); err != nil {
		return r.fail(t, fmt.Errorf("inject context: %w", err))
	}
	r.log.Info("context injected", "id", t.ID)

	// Step 4: Run the agent runtime
	result, err := r.runRuntime(instance.JailID, t, onEvent)
	if err != nil {
		return r.fail(t, fmt.Errorf("run runtime %q: %w", t.Runtime, err))
	}

	// Step 5: Collect results
	t.ExitCode = &result.ExitCode
	if result.ExitCode == 0 {
		t.Status = StatusCompleted
	} else {
		t.Status = StatusFailed
	}
	completedAt := time.Now()
	t.CompletedAt = &completedAt

	_ = result
	// Try to read output files
	r.collectResults(instance.JailID, t)

	if r.Metrics != nil {
		r.Metrics.ObserveTaskResult(t.Status == StatusCompleted, completedAt.Sub(*t.StartedAt))
	}
	evType := events.TypeTaskCompleted
	if t.Status != StatusCompleted {
		evType = events.TypeTaskFailed
	}
	r.emit(events.Event{
		ID:        t.ID,
		Type:      evType,
		Timestamp: completedAt,
		TaskID:    t.ID,
		VMName:    t.VMName,
		Data: map[string]interface{}{
			"exit_code":    result.ExitCode,
			"duration_sec": completedAt.Sub(*t.StartedAt).Seconds(),
			"cost_usd":     t.CostUSD,
			"files":        t.ResultFiles,
		},
	})

	r.log.Info("task completed",
		"id", t.ID,
		"status", t.Status,
		"exit_code", result.ExitCode,
		"duration", completedAt.Sub(*t.StartedAt),
	)

	return nil
}

// waitForAgent polls the guest agent via vsock until it responds.
func (r *Runner) waitForAgent(jailID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := vsock.Ping(jailID)
		if err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("agent did not respond within %s", timeout)
}

// injectContext writes credentials, settings, and env vars into the VM via vsock.
//
// Auth resolution order (first match wins):
//  1. ANTHROPIC_API_KEY env var set on the host — injected as env var into guest
//  2. OAuth credentials at r.CredentialsPath — injected as ~/.claude/.credentials.json
//
// An API key is often the right choice for unattended operation and multi-user
// servers; OAuth reuses the operator's Claude subscription quota and is the
// simplest path for a single user.
func (r *Runner) injectContext(jailID string, t *Task) error {
	var files []agent.FileEntry

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	useAPIKey := apiKey != ""

	if !useAPIKey {
		// Fall back to OAuth credentials on the host filesystem.
		credPath := r.CredentialsPath
		if credPath != "" {
			credData, err := os.ReadFile(credPath)
			if err != nil {
				return fmt.Errorf("read credentials %s: %w (and no ANTHROPIC_API_KEY set)", credPath, err)
			}
			files = append(files, agent.FileEntry{
				Path:    "/root/.claude/.credentials.json",
				Content: credData,
				Mode:    0600,
			})
		}
	}

	// Claude settings — allow all tools
	settings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []string{"Bash(*)", "Read", "Write", "Edit", "Glob", "Grep", "WebFetch", "WebSearch"},
			"deny":  []string{},
		},
	}
	settingsJSON, _ := json.MarshalIndent(settings, "", "  ")
	files = append(files, agent.FileEntry{
		Path:    "/root/.claude/settings.json",
		Content: settingsJSON,
		Mode:    0644,
	})

	// Environment script. Values are single-quoted (with embedded single quotes
	// escaped) so that metacharacters like `$`, backtick, `"` are not
	// interpreted by the shell at login time. Keys are validated against a
	// strict POSIX identifier regex.
	envScript := "export CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS=true\n"
	if useAPIKey {
		envScript += "export ANTHROPIC_API_KEY=" + shellSingleQuote(apiKey) + "\n"
	}
	for k, v := range t.EnvVars {
		if !envVarNameRE.MatchString(k) {
			return fmt.Errorf("invalid env var name %q", k)
		}
		envScript += "export " + k + "=" + shellSingleQuote(v) + "\n"
	}
	files = append(files, agent.FileEntry{
		Path:    "/etc/profile.d/claude.sh",
		Content: []byte(envScript),
		Mode:    0644,
	})

	// Task definition
	taskJSON, _ := json.MarshalIndent(t, "", "  ")
	files = append(files, agent.FileEntry{
		Path:    "/root/task/task.json",
		Content: taskJSON,
		Mode:    0644,
	})

	// Create output directory
	files = append(files, agent.FileEntry{
		Path:    filepath.Join(t.OutputDir, ".keep"),
		Content: []byte(""),
		Mode:    0644,
	})

	// Additional user-provided files
	for path, content := range t.Files {
		files = append(files, agent.FileEntry{
			Path:    path,
			Content: []byte(content),
			Mode:    0644,
		})
	}

	return vsock.WriteFiles(jailID, files)
}

// runRuntime dispatches to the configured Runtime to build and run the agent.
func (r *Runner) runRuntime(jailID string, t *Task, onEvent OnEvent) (*agent.ExecResult, error) {
	rt, err := r.Runtimes.New(t.Runtime)
	if err != nil {
		return nil, err
	}

	spec := runtime.PromptSpec{
		Prompt:       t.Prompt,
		MaxTurns:     t.MaxTurns,
		AllowedTools: t.AllowedTools,
		OutputDir:    t.OutputDir,
		EnvVars:      t.EnvVars,
	}
	inv := rt.Invocation(spec)

	// Write the prompt into the guest if the runtime wants to read it from a file.
	if inv.PromptFile != "" {
		if err := vsock.WriteFiles(jailID, []agent.FileEntry{{
			Path:    inv.PromptFile,
			Content: []byte(t.Prompt),
			Mode:    0644,
		}}); err != nil {
			return nil, fmt.Errorf("write prompt file: %w", err)
		}
	}

	var outputBuf strings.Builder

	result, err := vsock.ExecStream(jailID, inv.Command, inv.Env, inv.WorkDir, func(event agent.StreamEvent) {
		if event.Type == agent.StreamEventStdout || event.Type == agent.StreamEventStderr {
			outputBuf.WriteString(event.Data)
			outputBuf.WriteString("\n")
			stream := "stdout"
			if event.Type == agent.StreamEventStderr {
				stream = "stderr"
			}
			rt.ObserveLine(runtime.StreamSample{Stream: stream, Line: event.Data})
		}
		if onEvent != nil {
			onEvent(t.ID, event)
		}
	})
	if err != nil {
		return nil, err
	}

	t.Output = outputBuf.String()
	if summary := rt.Summary(); summary.CostUSD > 0 {
		t.CostUSD = summary.CostUSD
	}

	return result, nil
}

// collectResults downloads output files from the VM to the host.
func (r *Runner) collectResults(jailID string, t *Task) {
	// List files in output directory
	result, err := vsock.Exec(jailID, []string{"find", t.OutputDir, "-type", "f", "-not", "-name", ".keep"}, nil, "/root")
	if err != nil {
		r.log.Warn("failed to list output files", "error", err)
		return
	}

	guestFiles := strings.Split(strings.TrimSpace(result.Stdout), "\n")

	// Also check /root for any generated files (code, etc.)
	workResult, err := vsock.Exec(jailID, []string{"find", "/root", "-maxdepth", "2", "-type", "f",
		"-not", "-path", "/root/.claude/*",
		"-not", "-path", "/root/task/*",
		"-not", "-name", ".bashrc", "-not", "-name", ".profile",
		"-newer", "/tmp/claude-prompt.txt"}, nil, "/root")
	if err == nil && workResult.Stdout != "" {
		for _, f := range strings.Split(strings.TrimSpace(workResult.Stdout), "\n") {
			f = strings.TrimSpace(f)
			if f != "" && f != "/tmp/claude-prompt.txt" {
				guestFiles = append(guestFiles, f)
			}
		}
	}

	if len(guestFiles) == 0 || (len(guestFiles) == 1 && guestFiles[0] == "") {
		return
	}

	// Create host-side results directory
	hostDir := filepath.Join(ResultsDir, t.ID)
	if err := os.MkdirAll(hostDir, 0755); err != nil {
		r.log.Warn("failed to create results dir", "error", err)
		return
	}

	for _, guestPath := range guestFiles {
		guestPath = strings.TrimSpace(guestPath)
		if guestPath == "" {
			continue
		}

		// Read file from VM
		data, err := vsock.ReadFile(jailID, guestPath)
		if err != nil {
			r.log.Warn("failed to read file from VM", "path", guestPath, "error", err)
			continue
		}

		// Determine host filename — flatten path
		hostName := filepath.Base(guestPath)
		hostPath := filepath.Join(hostDir, hostName)

		// Handle duplicates
		if _, err := os.Stat(hostPath); err == nil {
			ext := filepath.Ext(hostName)
			base := strings.TrimSuffix(hostName, ext)
			for i := 2; ; i++ {
				hostPath = filepath.Join(hostDir, fmt.Sprintf("%s-%d%s", base, i, ext))
				if _, err := os.Stat(hostPath); os.IsNotExist(err) {
					break
				}
			}
			hostName = filepath.Base(hostPath)
		}

		if err := os.WriteFile(hostPath, data, 0644); err != nil {
			r.log.Warn("failed to write result file", "path", hostPath, "error", err)
			continue
		}

		t.ResultFiles = append(t.ResultFiles, hostName)
		r.log.Info("downloaded result file", "guest", guestPath, "host", hostPath, "size", len(data))
	}
}

// fail marks a task as failed.
func (r *Runner) fail(t *Task, err error) error {
	t.Status = StatusFailed
	t.Error = err.Error()
	now := time.Now()
	t.CompletedAt = &now
	r.log.Error("task failed", "id", t.ID, "error", err)
	r.emit(events.Event{
		ID:        t.ID,
		Type:      events.TypeTaskFailed,
		Timestamp: now,
		TaskID:    t.ID,
		VMName:    t.VMName,
		Data:      map[string]interface{}{"error": err.Error()},
	})
	return err
}

// emit sends an event to the configured Sink (if any). Non-blocking.
func (r *Runner) emit(ev events.Event) {
	if r.Events != nil {
		r.Events.Emit(ev)
	}
}

// shellSingleQuote wraps s in POSIX-compliant single quotes: every embedded
// single quote is encoded as `'"'"'` so the resulting string is safe to paste
// after `export VAR=` in a bash script. No variable expansion, command
// substitution, or backslash handling occurs inside single quotes — this is
// the strictest shell quoting available.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
