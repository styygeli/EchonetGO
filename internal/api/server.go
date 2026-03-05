package api

import (
	"encoding/json"
	"net/http"

	"github.com/styygeli/echonetgo/internal/logging"
)

var log = logging.New("api")

// Server provides HTTP endpoints for health and cached state.
type Server struct {
	ListenAddr string
	// GetState is called to return current cached state for API responses.
	// Can be nil; then /state returns {}.
	GetState func() interface{}
}

// Handler returns an http.Handler for the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/state", s.handleState)
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

// ListenAndServe starts the HTTP server. It blocks until the server is shut down.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{Addr: s.ListenAddr, Handler: s.Handler()}
	log.Infof("API listening on %s", s.ListenAddr)
	return srv.ListenAndServe()
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
	w.Header().Set("Content-Type", "application/json")
	var state interface{} = map[string]interface{}{}
	if s.GetState != nil {
		state = s.GetState()
	}
	if state == nil {
		state = map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(state)
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
<p><a href="/health">Health</a> | <a href="/state">State</a></p>
</body>
</html>`))
}
