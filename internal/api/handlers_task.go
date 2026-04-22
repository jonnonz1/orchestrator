package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jonnonz1/orchestrator/internal/agent"
	"github.com/jonnonz1/orchestrator/internal/task"
)

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	autoDestroy := true
	if req.AutoDestroy != nil {
		autoDestroy = *req.AutoDestroy
	}

	taskID := uuid.New().String()

	t := &task.Task{
		ID:           taskID,
		Prompt:       req.Prompt,
		VMName:       req.VMName,
		RamMB:        req.RamMB,
		VCPUs:        req.VCPUs,
		EnvVars:      req.EnvVars,
		Files:        req.Files,
		MaxTurns:     req.MaxTurns,
		AllowedTools: req.AllowedTools,
		AutoDestroy:  autoDestroy,
		OutputDir:    req.OutputDir,
		Timeout:      req.Timeout,
	}

	// Set up streaming to hub (per-task so concurrent tasks don't cross-contaminate)
	taskStream := s.streamHub.GetOrCreate(taskID)
	onEvent := func(id string, event agent.StreamEvent) {
		taskStream.Publish(event)
	}

	// Run task in background with a per-invocation callback
	go func() {
		s.taskRunner.Run(context.Background(), t, onEvent)
	}()

	writeJSON(w, http.StatusAccepted, t)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := s.taskStore.List()
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := s.taskStore.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := s.taskStore.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if t.Status == task.StatusRunning {
		// Destroy the VM to cancel
		if t.VMName != "" {
			s.vmMgr.Destroy(r.Context(), t.VMName)
		}
		t.Status = task.StatusCancelled
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetTaskFile serves a result file downloaded from a task's VM.
func (s *Server) handleGetTaskFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	// Verify task exists
	_, err := s.taskStore.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Serve the file
	filePath := filepath.Join(task.ResultsDir, id, filepath.Base(filename))
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	http.ServeFile(w, r, filePath)
}

// handleListTaskFiles lists result files for a task.
func (s *Server) handleListTaskFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	t, err := s.taskStore.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	type fileInfo struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	}

	var files []fileInfo
	for _, name := range t.ResultFiles {
		path := filepath.Join(task.ResultsDir, id, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			Name: name,
			URL:  "/api/v1/tasks/" + id + "/files/" + name,
			Size: info.Size(),
		})
	}

	writeJSON(w, http.StatusOK, files)
}
