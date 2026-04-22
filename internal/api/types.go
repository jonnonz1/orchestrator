package api

// CreateVMRequest is the request body for POST /api/v1/vms.
type CreateVMRequest struct {
	Name    string            `json:"name"`
	RamMB   int               `json:"ram_mb"`
	VCPUs   int               `json:"vcpus"`
	Files   map[string]string `json:"files,omitempty"`
	EnvVars map[string]string `json:"env_vars,omitempty"`
}

// CreateTaskRequest is the request body for POST /api/v1/tasks.
type CreateTaskRequest struct {
	Prompt       string            `json:"prompt"`
	VMName       string            `json:"vm_name,omitempty"`
	RamMB        int               `json:"ram_mb,omitempty"`
	VCPUs        int               `json:"vcpus,omitempty"`
	EnvVars      map[string]string `json:"env_vars,omitempty"`
	Files        map[string]string `json:"files,omitempty"`
	MaxTurns     int               `json:"max_turns,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
	AutoDestroy  *bool             `json:"auto_destroy,omitempty"`
	OutputDir    string            `json:"output_dir,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
}

// ExecRequest is the request body for POST /api/v1/vms/{name}/exec.
type ExecRequest struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// ErrorResponse is returned on API errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatsResponse is returned by GET /api/v1/stats.
type StatsResponse struct {
	TotalVMs   int `json:"total_vms"`
	RunningVMs int `json:"running_vms"`
	TotalTasks int `json:"total_tasks"`
}
