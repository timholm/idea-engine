// Package api provides an HTTP server for monitoring the idea-engine pipeline.
package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// Server is the monitoring HTTP API.
type Server struct {
	db   *db.DB
	mux  *http.ServeMux
}

// New creates a monitoring API server.
func New(database *db.DB) *Server {
	s := &Server{
		db:  database,
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

// Handler returns the HTTP handler for this server.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("GET /candidates", s.handleCandidates)
	s.mux.HandleFunc("GET /candidates/{arxivID}", s.handleCandidate)
	s.mux.HandleFunc("GET /specs", s.handleSpecs)
	s.mux.HandleFunc("GET /stats", s.handleStats)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get stats: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"stats":  stats,
	})
}

func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := 100

	candidates, err := s.db.ListCandidates(status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list candidates: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, candidates)
}

func (s *Server) handleCandidate(w http.ResponseWriter, r *http.Request) {
	arxivID := r.PathValue("arxivID")
	if arxivID == "" {
		writeError(w, http.StatusBadRequest, "arxivID is required")
		return
	}

	candidate, err := s.db.GetCandidate(arxivID)
	if err != nil {
		writeError(w, http.StatusNotFound, "candidate not found: "+err.Error())
		return
	}

	// Parse embedded JSON for richer response
	response := map[string]interface{}{
		"id":            candidate.ID,
		"arxiv_id":      candidate.ArxivID,
		"title":         candidate.Title,
		"status":        candidate.Status,
		"score":         candidate.Score,
		"discovered_at": candidate.DiscoveredAt,
		"delivered_at":  candidate.DeliveredAt,
	}

	if candidate.ResearchJSON != "" {
		var research types.ResearchContext
		if err := json.Unmarshal([]byte(candidate.ResearchJSON), &research); err == nil {
			response["research"] = research
		}
	}

	if candidate.SpecJSON != "" {
		var spec types.ProductSpec
		if err := json.Unmarshal([]byte(candidate.SpecJSON), &spec); err == nil {
			response["spec"] = spec
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSpecs(w http.ResponseWriter, r *http.Request) {
	candidates, err := s.db.GetSynthesizedSpecs(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get specs: "+err.Error())
		return
	}

	var specs []types.ProductSpec
	for _, c := range candidates {
		if c.SpecJSON == "" {
			continue
		}
		var spec types.ProductSpec
		if err := json.Unmarshal([]byte(c.SpecJSON), &spec); err != nil {
			log.Printf("[api] warning: invalid spec JSON for %s: %v", c.ArxivID, err)
			continue
		}
		specs = append(specs, spec)
	}

	writeJSON(w, http.StatusOK, specs)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get stats: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api] error encoding response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
