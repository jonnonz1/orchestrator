// Package runtime defines the pluggable agent runtime abstraction.
//
// A Runtime is the bridge between a task prompt and a process running inside
// a guest VM. Orchestrator ships two runtimes out of the box:
//
//   - "claude": invokes the Claude Code CLI with stream-json output and
//     parses cost/turns from the JSON stream.
//   - "shell": runs the prompt as a bash -c invocation with no interpretation.
//     Useful for non-agent workloads (build-and-screenshot, data processing).
//
// Adding a new runtime is ~50 lines: implement Runtime and register via
// Register() (or hand it directly to the task runner).
package runtime

import (
	"fmt"
)

// PromptSpec carries everything a Runtime needs to build its invocation.
// It is a subset of task.Task, kept here to avoid a cyclic dependency.
type PromptSpec struct {
	Prompt       string
	MaxTurns     int
	AllowedTools []string
	OutputDir    string
	EnvVars      map[string]string
}

// Invocation describes a single process to run inside the guest VM.
type Invocation struct {
	// Command is the argv, e.g. ["bash", "-c", "claude -p ..."].
	Command []string

	// Env are environment variables to set for the process. These augment
	// (and override) the VM-wide env injected into /etc/profile.d/.
	Env map[string]string

	// WorkDir is the working directory inside the guest. Empty = guest default.
	WorkDir string

	// PromptFile is a path inside the guest where the raw prompt is written
	// before the command runs. Runtimes may reference it in Command.
	// If empty, no prompt file is written.
	PromptFile string
}

// StreamSample is a single line of output from the running process. Runtimes
// get a chance to inspect output (e.g. parsing cost metadata) as it streams.
type StreamSample struct {
	Stream string // "stdout" or "stderr"
	Line   string
}

// Summary is post-run metadata extracted from the stream.
type Summary struct {
	CostUSD float64
}

// Runtime is the contract every agent backend implements.
type Runtime interface {
	// Name returns the runtime identifier, e.g. "claude".
	Name() string

	// Invocation builds the process spec from a prompt spec.
	Invocation(spec PromptSpec) Invocation

	// ObserveLine is called for each line streamed from the guest.
	// Runtimes may update internal state (e.g. accumulating cost).
	// Return true to suppress the line from user-facing output (rare).
	ObserveLine(sample StreamSample) bool

	// Summary returns accumulated metadata after the process exits.
	Summary() Summary
}

// Registry is a thread-safe map of runtime names to constructors. Constructors
// are called per-task so each task gets a fresh Runtime instance (and therefore
// fresh cost accumulators).
type Registry struct {
	entries map[string]func() Runtime
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]func() Runtime)}
}

// Register adds a runtime constructor. Panics on duplicate names.
func (r *Registry) Register(name string, ctor func() Runtime) {
	if _, exists := r.entries[name]; exists {
		panic(fmt.Sprintf("runtime %q already registered", name))
	}
	r.entries[name] = ctor
}

// New returns a fresh instance of the named runtime.
func (r *Registry) New(name string) (Runtime, error) {
	ctor, ok := r.entries[name]
	if !ok {
		return nil, fmt.Errorf("unknown runtime %q (available: %v)", name, r.Names())
	}
	return ctor(), nil
}

// Names returns all registered runtime names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.entries))
	for n := range r.entries {
		names = append(names, n)
	}
	return names
}

// Default is the package-level registry, pre-populated in init.go.
var Default = NewRegistry()
