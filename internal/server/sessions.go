package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) sessionsFilePath() string {
	return filepath.Join(s.selfDir, "data", "sessions.json")
}

func (s *Server) handleSessionsGet(w http.ResponseWriter, r *http.Request) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	raw, err := os.ReadFile(s.sessionsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to read sessions.json: "+err.Error())
		return
	}

	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}})
		return
	}
	if list == nil {
		list = []json.RawMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *Server) handleSessionsPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sessions json.RawMessage `json:"sessions"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.Sessions) == 0 {
		body.Sessions = json.RawMessage("[]")
	}

	var check []json.RawMessage
	if err := json.Unmarshal(body.Sessions, &check); err != nil {
		writeError(w, http.StatusBadRequest, "sessions must be a JSON array")
		return
	}

	pretty, err := json.MarshalIndent(check, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	targetPath := s.sessionsFilePath()
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create data directory: "+err.Error())
		return
	}
	if err := os.WriteFile(targetPath, pretty, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save sessions.json: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
