# Runtime plugin API

A **runtime** tells the task runner how to invoke an agent inside a VM. The
interface is three methods; the contract is documented below.

## Interface

From `internal/runtime/runtime.go`:

```go
package runtime

// PromptSpec is what the task runner hands to a runtime.
type PromptSpec struct {
    Prompt       string            // user prompt text
    MaxTurns     int               // 0 = unlimited (for agents that honour turn caps)
    AllowedTools []string          // for agents with a tool-allowlist concept
    OutputDir    string            // convention for writing result files
    EnvVars      map[string]string // user-supplied env vars (already validated)
}

// Invocation is what the runtime returns: the command the guest agent will
// actually exec.
type Invocation struct {
    Command    []string          // argv; the first element is the binary
    Env        map[string]string // additional env for this process only
    WorkDir    string            // cwd inside the guest (typically /root)
    PromptFile string            // optional: dropped into the guest before exec
}

// StreamSample is a line of stdout or stderr as it streams.
type StreamSample struct {
    Stream string // "stdout" or "stderr"
    Line   string
}

// Summary is whatever the runtime wants to surface at the end.
type Summary struct {
    CostUSD float64 // Claude reports this; other runtimes can leave zero.
}

// Runtime is the thing a plugin implements.
type Runtime interface {
    Invocation(PromptSpec) Invocation
    ObserveLine(StreamSample)
    Summary() Summary
}
```

A runtime instance is **not** shared across tasks — the registry creates a
fresh instance per task via a factory function. That means you can safely
accumulate state in struct fields.

## Registering a runtime

```go
package myruntime

import "github.com/jonnonz1/orchestrator/internal/runtime"

func init() {
    runtime.Default.Register("aider", func() runtime.Runtime { return &Aider{} })
}
```

`runtime.Default` is the registry the task runner consults. Override at
process startup if you want a bespoke registry.

Factories are called once per `task.Runner.Run` invocation.

## What to build in `Invocation`

Keep it simple — the guest agent is a dumb exec wrapper. The command you
return will be invoked under `bash -c` by the agent. If you need shell
semantics (redirection, piping), include them in the argv; if you don't,
skip `bash -c` entirely.

For long prompts, return a `PromptFile` and reference it in the command:

```go
return runtime.Invocation{
    PromptFile: "/tmp/aider-prompt.txt",
    Command:    []string{"aider", "--yes", "--no-pretty", "--message-file", "/tmp/aider-prompt.txt"},
    WorkDir:    "/root/workspace",
}
```

The task runner will `write_files` the prompt into the guest before the
agent execs your command.

## ObserveLine — streaming output

You get every stdout/stderr line as it arrives, one call per line, already
split at newline boundaries. The task runner **also** accumulates these into
`task.Output` so you don't need to — use `ObserveLine` for side effects
(parsing JSON events, tracking cost, etc.).

Example: pull the last `result` event out of Claude's stream-json:

```go
func (c *ClaudeRuntime) ObserveLine(s runtime.StreamSample) {
    if s.Stream != "stdout" { return }
    var ev struct {
        Type    string  `json:"type"`
        CostUSD float64 `json:"total_cost_usd"`
    }
    if err := json.Unmarshal([]byte(s.Line), &ev); err != nil { return }
    if ev.Type == "result" {
        c.summary.CostUSD = ev.CostUSD
    }
}
```

## Summary — after the run

Called exactly once, after the process exits. Return whatever you want
surfaced on the `task.Task.CostUSD` field. Everything else (exit code,
result files, duration) is managed by the runner.

## Packaging

Runtimes live in Go code that's compiled into the host binary. There's no
dlopen / plugin-loading mechanism. Package your runtime as a Go module and
either:

1. **Fork** orchestrator and add `myruntime "your-module"` as a blank import
   in the `cmd/orchestrator/main.go`.
2. **Wrap** orchestrator — write your own `main` that imports
   `github.com/jonnonz1/orchestrator/...` and registers your runtime before
   dispatching to the built-in commands.

Option 2 is the right pattern for anything you intend to maintain.

## Guest-side requirements

Your runtime's `Command[0]` has to exist in the rootfs. The ship-with rootfs
includes:

- `claude` (Claude Code)
- `node`, `npm`, `npx`
- `python3`, `pip`
- `chromium`
- `git`, `curl`, `wget`, `jq`, `make`, `tar`, `gzip`, `unzip`, `xz`
- `bash` (and `sh`)

For anything else, either rebuild the rootfs with your tool pre-installed
(edit `scripts/build-rootfs.sh`) or install it in the task prompt itself.

## Reference implementation

[`internal/runtime/claude.go`](https://github.com/jonnonz1/orchestrator/blob/main/internal/runtime/claude.go)
is ~70 lines and handles prompt-file injection, streaming JSON parsing, and
cost extraction. Copy-and-adapt for your own adapter.
