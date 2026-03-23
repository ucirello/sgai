package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type apiActionRunRequest struct {
	Name      string            `json:"name"`
	Variables map[string]string `json:"variables,omitempty"`
}

func (s *Server) handleAPIActionRun(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiActionRunRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "action name is required", http.StatusBadRequest)
		return
	}

	result := s.actionRunService(workspacePath, req.Name, req.Variables)
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, apiAdhocResponse{
		Running: result.Running,
		Output:  result.Output,
		Message: result.Message,
	})
}
