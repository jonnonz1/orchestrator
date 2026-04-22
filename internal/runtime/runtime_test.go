package runtime

import (
	"strings"
	"testing"
)

func TestDefaultRegistryHasClaudeAndShell(t *testing.T) {
	names := Default.Names()
	has := func(n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}
	if !has("claude") {
		t.Error("claude runtime not registered")
	}
	if !has("shell") {
		t.Error("shell runtime not registered")
	}
}

func TestClaudeInvocationIncludesPromptFile(t *testing.T) {
	rt := NewClaude()
	inv := rt.Invocation(PromptSpec{Prompt: "hello", MaxTurns: 5})
	if inv.PromptFile == "" {
		t.Fatal("claude runtime should set PromptFile")
	}
	joined := strings.Join(inv.Command, " ")
	if !strings.Contains(joined, "--max-turns 5") {
		t.Errorf("max-turns not wired: %s", joined)
	}
	if !strings.Contains(joined, inv.PromptFile) {
		t.Errorf("prompt file not referenced in command: %s", joined)
	}
}

func TestClaudeObservesCost(t *testing.T) {
	rt := NewClaude().(*Claude)
	rt.ObserveLine(StreamSample{Stream: "stdout", Line: `{"total_cost_usd":0.1234,"foo":"bar"}`})
	if got := rt.Summary().CostUSD; got != 0.1234 {
		t.Errorf("cost = %v, want 0.1234", got)
	}
}

func TestShellInvocationPassesPromptVerbatim(t *testing.T) {
	inv := NewShell().Invocation(PromptSpec{Prompt: "echo hi"})
	if len(inv.Command) != 3 || inv.Command[2] != "echo hi" {
		t.Fatalf("unexpected shell invocation: %v", inv.Command)
	}
}

func TestUnknownRuntimeErrors(t *testing.T) {
	if _, err := Default.New("nope"); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}
