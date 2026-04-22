package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/jonnonz1/orchestrator/internal/task"
	"github.com/jonnonz1/orchestrator/internal/vsock"
)

func (s *Server) handleRunTask(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	prompt := req.GetString("prompt", "")
	if prompt == "" {
		return mcplib.NewToolResultError("prompt is required"), nil
	}

	t := &task.Task{
		Prompt:      prompt,
		RamMB:       req.GetInt("ram_mb", 2048),
		VCPUs:       req.GetInt("vcpus", 2),
		Timeout:     req.GetInt("timeout", 600),
		MaxTurns:    req.GetInt("max_turns", 0),
		OutputDir:   req.GetString("output_dir", "/root/output"),
		AutoDestroy: true,
	}

	s.log.Info("MCP: running task", "prompt_len", len(prompt))

	// onEvent=nil — MCP is a synchronous tool call, the full output lands in t.Output.
	if err := s.taskRunner.Run(ctx, t, nil); err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("task failed: %v", err)), nil
	}

	// Build result
	result := map[string]interface{}{
		"task_id":      t.ID,
		"status":       t.Status,
		"exit_code":    t.ExitCode,
		"result_files": t.ResultFiles,
	}
	if t.CostUSD > 0 {
		result["cost_usd"] = t.CostUSD
	}
	if t.CompletedAt != nil && t.StartedAt != nil {
		result["duration_seconds"] = t.CompletedAt.Sub(*t.StartedAt).Seconds()
	}

	// Include the last part of output (truncated if very long)
	taskOutput := t.Output
	if len(taskOutput) > 4000 {
		taskOutput = "...(truncated)...\n" + taskOutput[len(taskOutput)-4000:]
	}
	result["output"] = taskOutput

	// If there are result files, include small text files inline
	// and note that binary files can be retrieved with get_task_file
	if len(t.ResultFiles) > 0 {
		result["hint"] = "Use get_task_file to retrieve file contents. For images, it returns base64-encoded data."
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleListVMs(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	instances := s.vmMgr.List()

	type vmSummary struct {
		Name    string `json:"name"`
		State   string `json:"state"`
		RamMB   int    `json:"ram_mb"`
		VCPUs   int    `json:"vcpus"`
		GuestIP string `json:"guest_ip"`
		PID     int    `json:"pid"`
	}

	summaries := make([]vmSummary, len(instances))
	for i, inst := range instances {
		summaries[i] = vmSummary{
			Name:    inst.Name,
			State:   string(inst.State),
			RamMB:   inst.RamMB,
			VCPUs:   inst.VCPUs,
			GuestIP: inst.GuestIP,
			PID:     inst.PID,
		}
	}

	data, _ := json.MarshalIndent(summaries, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleGetTaskStatus(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	taskID := req.GetString("task_id", "")
	if taskID == "" {
		return mcplib.NewToolResultError("task_id is required"), nil
	}

	t, err := s.taskStore.Get(taskID)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	data, _ := json.MarshalIndent(t, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleExecInVM(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	vmName := req.GetString("vm_name", "")
	command := req.GetString("command", "")

	if vmName == "" || command == "" {
		return mcplib.NewToolResultError("vm_name and command are required"), nil
	}

	instance, err := s.vmMgr.Get(vmName)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	result, err := vsock.Exec(instance.JailID, []string{"bash", "-c", command}, nil, "/root")
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("exec failed: %v", err)), nil
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleReadVMFile(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	vmName := req.GetString("vm_name", "")
	path := req.GetString("path", "")

	if vmName == "" || path == "" {
		return mcplib.NewToolResultError("vm_name and path are required"), nil
	}

	instance, err := s.vmMgr.Get(vmName)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	content, err := vsock.ReadFile(instance.JailID, path)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(content)), nil
}

func (s *Server) handleDestroyVM(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	vmName := req.GetString("vm_name", "")
	if vmName == "" {
		return mcplib.NewToolResultError("vm_name is required"), nil
	}

	if err := s.vmMgr.Destroy(ctx, vmName); err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	return mcplib.NewToolResultText(fmt.Sprintf(`{"status": "destroyed", "vm_name": "%s"}`, vmName)), nil
}

// handleListTaskFiles lists all result files for a completed task.
func (s *Server) handleListTaskFiles(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	taskID := req.GetString("task_id", "")
	if taskID == "" {
		return mcplib.NewToolResultError("task_id is required"), nil
	}

	t, err := s.taskStore.Get(taskID)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	type fileInfo struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		MimeType string `json:"mime_type"`
	}

	var files []fileInfo
	for _, name := range t.ResultFiles {
		path := filepath.Join(task.ResultsDir, taskID, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		mimeType := mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		files = append(files, fileInfo{
			Name:     name,
			Size:     info.Size(),
			MimeType: mimeType,
		})
	}

	data, _ := json.MarshalIndent(files, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

// handleGetTaskFile returns the contents of a result file.
// Text files are returned as text. Binary files (images, etc.) are returned
// as base64 with the MCP image content type so Claude can see them.
func (s *Server) handleGetTaskFile(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	taskID := req.GetString("task_id", "")
	filename := req.GetString("filename", "")

	if taskID == "" || filename == "" {
		return mcplib.NewToolResultError("task_id and filename are required"), nil
	}

	// Verify task exists
	_, err := s.taskStore.Get(taskID)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	// Read the file
	filePath := filepath.Join(task.ResultsDir, taskID, filepath.Base(filename))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("file not found: %s", filename)), nil
	}

	mimeType := mime.TypeByExtension(filepath.Ext(filename))

	// For images, return as MCP image content so Claude can see them
	if isImageMime(mimeType) {
		encoded := base64.StdEncoding.EncodeToString(data)
		return mcplib.NewToolResultImage("Screenshot/image from task "+taskID, encoded, mimeType), nil
	}

	// For text files, return as text
	if isTextFile(filename, mimeType) {
		return mcplib.NewToolResultText(string(data)), nil
	}

	// For other binary files, return base64 with metadata
	encoded := base64.StdEncoding.EncodeToString(data)
	result := map[string]interface{}{
		"filename":  filename,
		"mime_type": mimeType,
		"size":      len(data),
		"encoding":  "base64",
		"data":      encoded,
	}
	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return mcplib.NewToolResultText(string(jsonData)), nil
}

func isImageMime(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

func isTextFile(filename, mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	textExts := map[string]bool{
		".json": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
		".go": true, ".py": true, ".rb": true, ".rs": true, ".java": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".html": true, ".css": true, ".scss": true, ".less": true,
		".md": true, ".txt": true, ".yaml": true, ".yml": true, ".toml": true,
		".xml": true, ".csv": true, ".sql": true, ".sh": true, ".bash": true,
		".env": true, ".gitignore": true, ".dockerfile": true,
		".vue": true, ".svelte": true, ".astro": true,
	}
	return textExts[strings.ToLower(filepath.Ext(filename))]
}
