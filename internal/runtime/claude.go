package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Claude is the runtime for Anthropic's Claude Code CLI.
type Claude struct {
	cost float64
}

// NewClaude returns a fresh Claude runtime.
func NewClaude() Runtime { return &Claude{} }

// Name implements Runtime.
func (c *Claude) Name() string { return "claude" }

// Invocation implements Runtime.
func (c *Claude) Invocation(spec PromptSpec) Invocation {
	promptFile := "/tmp/claude-prompt.txt"

	args := fmt.Sprintf(`claude -p "$(cat %s)" --output-format stream-json --verbose`, promptFile)
	if spec.MaxTurns > 0 {
		args += fmt.Sprintf(" --max-turns %d", spec.MaxTurns)
	}
	if len(spec.AllowedTools) > 0 {
		args += " --allowedTools " + strings.Join(spec.AllowedTools, ",")
	}

	return Invocation{
		Command:    []string{"bash", "-c", "source /etc/profile.d/claude.sh && " + args},
		Env:        map[string]string{"CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS": "true", "HOME": "/root"},
		WorkDir:    "/root",
		PromptFile: promptFile,
	}
}

// ObserveLine parses cost from Claude's stream-json output.
func (c *Claude) ObserveLine(s StreamSample) bool {
	if !strings.Contains(s.Line, "total_cost_usd") {
		return false
	}
	var parsed struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal([]byte(s.Line), &parsed); err == nil && parsed.TotalCostUSD > 0 {
		c.cost = parsed.TotalCostUSD
	}
	return false
}

// Summary implements Runtime.
func (c *Claude) Summary() Summary { return Summary{CostUSD: c.cost} }
