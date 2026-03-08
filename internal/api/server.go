package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/styygeli/echonetgo/internal/logging"
)

var apiLog = logging.New("api")

// Server provides HTTP endpoints for health and cached state.
type Server struct {
	ListenAddr string
	// GetState is called to return current cached state for API responses.
	// Can be nil; then /state returns {}.
	GetState func() any
	// MetricsHandler serves /metrics in Prometheus text format.
	// If nil, the /metrics endpoint is not registered.
	MetricsHandler http.Handler
}

// Handler returns an http.Handler for the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/state", s.handleState)
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

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var state any = struct{}{}
	if s.GetState != nil {
		state = s.GetState()
	}
	if state == nil {
		state = struct{}{}
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(state); err != nil {
		apiLog.Errorf("JSON encode /state: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Link", `</metrics>; rel="successor-version"`)
	_, _ = w.Write(buf.Bytes())
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
<p><a href="/health">Health</a> | <a href="/metrics">Metrics</a> | <a href="/state">State (deprecated)</a></p>
</body>
</html>`))
}
