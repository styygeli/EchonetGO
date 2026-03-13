package api

import (
	"encoding/json"
	"net/http"

	"github.com/styygeli/echonetgo/internal/logging"
)

var apiLog = logging.New("api")

// Server provides HTTP endpoints for health and metrics.
type Server struct {
	ListenAddr string
	// MetricsHandler serves /metrics in Prometheus text format.
	// If nil, the /metrics endpoint is not registered.
	MetricsHandler http.Handler
	// Readiness is optional; if set, /ready reports component readiness.
	Readiness *Readiness
}

// Handler returns an http.Handler for the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	if s.MetricsHandler != nil {
		mux.Handle("/metrics", s.MetricsHandler)
	}
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.Readiness == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready","components":{}}`))
		return
	}
	allReady, components := s.Readiness.Status()
	// Map bool to "ready" / "not_ready" for response
	compStatus := make(map[string]string, len(components))
	for name, ready := range components {
		if ready {
			compStatus[name] = "ready"
		} else {
			compStatus[name] = "not_ready"
		}
	}
	body := struct {
		Status     string            `json:"status"`
		Components map[string]string `json:"components"`
	}{
		Status:     "not_ready",
		Components: compStatus,
	}
	if allReady {
		body.Status = "ready"
	}
	data, _ := json.Marshal(body)
	if allReady {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_, _ = w.Write(data)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>EchonetGO</title></head>
<body>
<h1>EchonetGO</h1>
<p><a href="/health">Health</a> | <a href="/ready">Ready</a> | <a href="/metrics">Metrics</a></p>
</body>
</html>`))
}
