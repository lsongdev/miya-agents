package acp

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Server represents an ACP HTTP server.
type Server struct {
	agent Agent
	mux   *http.ServeMux
}

// NewServer creates a new ACP server.
func NewServer(agent Agent) *Server {
	s := &Server{
		agent: agent,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /agents", s.handleListAgents)
	s.mux.HandleFunc("GET /agents/{name}", s.handleGetAgent)
	s.mux.HandleFunc("POST /runs", s.handleCreateRun)
	s.mux.HandleFunc("GET /runs/{run_id}", s.handleGetRun)
	s.mux.HandleFunc("POST /runs/{run_id}", s.handleResumeRun)
	s.mux.HandleFunc("POST /runs/{run_id}/cancel", s.handleCancelRun)
	s.mux.HandleFunc("GET /session/{session_id}", s.handleGetSession)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.agent.ListAgents(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, agents)
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	agent, err := s.agent.GetAgent(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, agent)
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req RunCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	run, err := s.agent.CreateRun(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Mode == ModeStream {
		s.handleStreamRun(w, r, run.RunID)
		return
	}

	jsonResponse(w, run)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	run, err := s.agent.GetRun(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, run)
}

func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	var req RunResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	run, err := s.agent.ResumeRun(r.Context(), runID, req.AwaitResume)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	run, err := s.agent.CancelRun(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	jsonResponse(w, run)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	session, err := s.agent.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, session)
}

func (s *Server) handleStreamRun(w http.ResponseWriter, r *http.Request, runID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	events, err := s.agent.StreamRun(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			data, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
			flusher.Flush()

			// If run is completed, we can close the stream
			if event.Type == "run.completed" || event.Type == "run.failed" {
				return
			}
		}
	}
}

// Serve starts the ACP server on the given address.
func (s *Server) Serve(addr string) error {
	return http.ListenAndServe(addr, s)
}

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
