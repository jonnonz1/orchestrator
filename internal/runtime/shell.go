package runtime

// Shell is a minimal runtime that runs the prompt as a bash -c invocation.
// Useful for non-agent workloads (build-and-screenshot, data processing,
// pre-baked scripts).
type Shell struct{}

// NewShell returns a shell runtime.
func NewShell() Runtime { return &Shell{} }

// Name implements Runtime.
func (Shell) Name() string { return "shell" }

// Invocation implements Runtime.
func (Shell) Invocation(spec PromptSpec) Invocation {
	return Invocation{
		Command: []string{"bash", "-c", spec.Prompt},
		Env:     map[string]string{"HOME": "/root"},
		WorkDir: "/root",
	}
}

// ObserveLine implements Runtime.
func (Shell) ObserveLine(StreamSample) bool { return false }

// Summary implements Runtime.
func (Shell) Summary() Summary { return Summary{} }
