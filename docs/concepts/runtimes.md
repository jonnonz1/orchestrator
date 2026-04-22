# Runtimes

A **runtime** is the plugin that tells the task runner how to actually invoke
an agent inside a VM. Orchestrator ships two out of the box:

| Name | What it runs | When to use |
|---|---|---|
| `claude` (default) | Claude Code with `--dangerously-skip-permissions`, streaming stream-json output. | AI agent tasks. |
| `shell` | A plain `bash -c` of the prompt. | Non-agent batch work — curl, scraping, compilation. |

## The runtime interface

A runtime is a Go type that satisfies `runtime.Runtime`:

```go
type Runtime interface {
    // Invocation builds the exec spec for the guest agent from the user's
    // task spec: command argv, env vars, working directory, optional prompt
    // file to drop in the guest.
    Invocation(PromptSpec) Invocation

    // ObserveLine gets every stdout/stderr line as it streams. Useful for
    // parsing JSON events (cost, token counts, tool calls).
    ObserveLine(StreamSample)

    // Summary returns the runtime-specific summary at the end of the run.
    // Claude uses this to report CostUSD. Other runtimes can leave it zero.
    Summary() Summary
}
```

The `claude` adapter parses Claude Code's `--output-format stream-json`
events to pull out the final cost, surface tool calls, and hand off readable
output to the dashboard. The `shell` adapter just drops the prompt into
`bash -c` and doesn't parse anything.

## Adding your own runtime

A minimal runtime is about 50 lines. Register it from the main package or
via a side-effect import before the task runner starts.

```go
package myruntime

import (
    "github.com/jonnonz1/orchestrator/internal/runtime"
)

type Aider struct {
    summary runtime.Summary
}

func (a *Aider) Invocation(spec runtime.PromptSpec) runtime.Invocation {
    return runtime.Invocation{
        Command: []string{
            "bash", "-lc",
            "aider --yes --no-pretty --message " + shellQuote(spec.Prompt),
        },
        Env:     spec.EnvVars,
        WorkDir: "/root",
    }
}

func (a *Aider) ObserveLine(s runtime.StreamSample) {
    // Optionally parse lines here, accumulate summary state.
}

func (a *Aider) Summary() runtime.Summary {
    return a.summary
}

func init() {
    runtime.Default.Register("aider", func() runtime.Runtime { return &Aider{} })
}
```

Submit the task with `--runtime aider` on the CLI, or
`"runtime": "aider"` in the REST `/api/v1/tasks` payload.

Full API reference: [Runtime plugin API](../reference/runtime-plugin.md).

## Choosing between Claude and shell

| Use the `claude` runtime when… | Use the `shell` runtime when… |
|---|---|
| The task needs reasoning, multi-step tool use, or natural-language synthesis. | The task is deterministic scripted work. |
| You want per-task cost accounting in dollars. | You just want exit code + stdout. |
| You want tool-call visualisation in the dashboard. | Simplicity beats observability. |
| The prompt is long-form and multi-turn (`--max-turns`). | The prompt is a single shell pipeline. |

## Guest-side requirements

The runtime's command has to exist in the guest rootfs. The default rootfs
ships with `claude`, `node`, `python3`, `chromium`, `git`, `curl`, `bash`,
`jq`, `make`, `cargo` (arm64 / amd64). For custom runtimes you'll either
need to install the binary into the rootfs (edit `scripts/build-rootfs.sh`
and rebuild) or `apt-get install` it as a first step in the task prompt.

## Runtime-specific gotchas

### Claude

- OAuth credentials are copied from
  `$HOME/.claude/.credentials.json` unless `ANTHROPIC_API_KEY` is set.
- A `settings.json` with all tools allowed is injected at `/root/.claude/settings.json`.
- `CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS=true` is set in the guest profile.
- The `cost_usd` you see in task output comes from the stream-json `result`
  event at the end of the run.

### Shell

- Prompt is passed to `bash -c` — beware of single-quote handling inside the
  prompt. The orchestrator wraps the whole thing safely, but you still need
  POSIX-correct shell inside.
- Exit code maps 1:1 to task `exit_code`. Any non-zero counts as task
  "failed".
