package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jonnonz1/orchestrator/internal/vm"
	"github.com/jonnonz1/orchestrator/internal/vsock"
)

func (s *Server) handleCreateVM(w http.ResponseWriter, r *http.Request) {
	var req CreateVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	instance, err := s.vmMgr.Create(r.Context(), vm.VMConfig{
		Name:  req.Name,
		RamMB: req.RamMB,
		VCPUs: req.VCPUs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, instance)
}

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request) {
	instances := s.vmMgr.List()
	writeJSON(w, http.StatusOK, instances)
}

func (s *Server) handleGetVM(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	instance, err := s.vmMgr.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.vmMgr.Destroy(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStopVM(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.vmMgr.Stop(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	instance, _ := s.vmMgr.Get(name)
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleExecVM(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	instance, err := s.vmMgr.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	result, err := vsock.Exec(instance.JailID, []string{"bash", "-c", req.Command}, nil, "/root")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
