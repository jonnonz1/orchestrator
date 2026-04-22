package task

import (
	"time"
)

// Status represents the state of a task.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Task represents an agent task running inside a VM.
type Task struct {
	ID           string            `json:"id"`
	Status       Status            `json:"status"`
	Prompt       string            `json:"prompt"`
	Runtime      string            `json:"runtime,omitempty"` // "claude" (default), "shell", etc.
	VMName       string            `json:"vm_name"`
	RamMB        int               `json:"ram_mb"`
	VCPUs        int               `json:"vcpus"`
	EnvVars      map[string]string `json:"env_vars,omitempty"`
	Files        map[string]string `json:"files,omitempty"`
	MaxTurns     int               `json:"max_turns,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
	AutoDestroy  bool              `json:"auto_destroy"`
	OutputDir    string            `json:"output_dir"`
	Timeout      int               `json:"timeout"`

	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	ResultFiles []string   `json:"result_files,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CostUSD     float64    `json:"cost_usd,omitempty"`
}

// Defaults applies default values.
func (t *Task) Defaults() {
	if t.RamMB == 0 {
		t.RamMB = 2048
	}
	if t.VCPUs == 0 {
		t.VCPUs = 2
	}
	if t.OutputDir == "" {
		t.OutputDir = "/root/output"
	}
	if t.Timeout == 0 {
		t.Timeout = 600
	}
	if t.Runtime == "" {
		t.Runtime = "claude"
	}
}
