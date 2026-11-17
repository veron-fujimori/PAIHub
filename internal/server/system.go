package server

import (
	"net/http"
	"runtime"
	"strings"

	"paihub/internal/config"
	"paihub/internal/sysenv"
)

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	running, activeAgent := s.agentMgr.IsRunning()
	cfg := s.cfgStore.Get()

	writeJSON(w, http.StatusOK, map[string]any{
		"version":         AppVersion,
		"allocMB":         round2(float64(m.Alloc) / 1024 / 1024),
		"heapInuseMB":     round2(float64(m.HeapInuse) / 1024 / 1024),
		"sysMB":           round2(float64(m.Sys) / 1024 / 1024),
		"numGoroutine":    runtime.NumGoroutine(),
		"aiRunning":       running,
		"activeAgent":     activeAgent,
		"localIPs":        sysenv.LocalIPs(),
		"port":            cfg.Port,
		"selfDir":         s.selfDir,
		"activeWorkspace": cfg.ActiveWorkspace,
		"defaultAgent":    cfg.DefaultAgent,
		"defaultModel":    cfg.DefaultModel,
		"defaultEffort":   cfg.DefaultEffort,
		"pinRequired":     cfg.Pin != "",
	})
}

func (s *Server) handleDefaultsGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfgStore.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"defaultAgent":  cfg.DefaultAgent,
		"defaultModel":  cfg.DefaultModel,
		"defaultEffort": cfg.DefaultEffort,
	})
}

func (s *Server) handleDefaultsSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DefaultAgent  string `json:"defaultAgent"`
		DefaultModel  string `json:"defaultModel"`
		DefaultEffort string `json:"defaultEffort"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agID := strings.TrimSpace(body.DefaultAgent)
	if agID != "" {
		if _, ok := s.agentMgr.Get(agID); !ok {
			writeError(w, http.StatusBadRequest, "default agent not found")
			return
		}
	}
	if err := s.cfgStore.Update(func(c *config.Config) {
		if agID != "" {
			c.DefaultAgent = agID
		}
		c.DefaultModel = strings.TrimSpace(body.DefaultModel)
		c.DefaultEffort = strings.TrimSpace(body.DefaultEffort)
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg := s.cfgStore.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"defaultAgent":  cfg.DefaultAgent,
		"defaultModel":  cfg.DefaultModel,
		"defaultEffort": cfg.DefaultEffort,
	})
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
