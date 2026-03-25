// Package api provides an HTTP server for monitoring the idea-engine fusion pipeline.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// Server is the monitoring HTTP API.
type Server struct {
	db  *db.DB
	mux *http.ServeMux
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
	s.mux.HandleFunc("GET /clusters", s.handleClusters)
	s.mux.HandleFunc("GET /clusters/{id}", s.handleCluster)
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
		"mode":   "fusion",
		"stats":  stats,
	})
}

func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := 100

	clusters, err := s.db.ListClusters(status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list clusters: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, clusters)
}

func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "cluster id is required")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster id")
		return
	}

	cluster, err := s.db.GetCluster(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "cluster not found: "+err.Error())
		return
	}

	// Parse embedded JSON for richer response
	response := map[string]interface{}{
		"id":            cluster.ID,
		"problem_space": cluster.ProblemSpace,
		"paper_ids":     cluster.PaperIDs,
		"status":        cluster.Status,
		"score":         cluster.Score,
		"created_at":    cluster.CreatedAt,
		"delivered_at":  cluster.DeliveredAt,
	}

	if cluster.ResearchJSON != "" {
		var research types.FusionResearchContext
		if err := json.Unmarshal([]byte(cluster.ResearchJSON), &research); err == nil {
			response["research"] = research
		}
	}

	if cluster.SpecJSON != "" {
		var spec types.ProductSpec
		if err := json.Unmarshal([]byte(cluster.SpecJSON), &spec); err == nil {
			response["spec"] = spec
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSpecs(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.db.GetSynthesizedClusters(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get specs: "+err.Error())
		return
	}

	var specs []types.ProductSpec
	for _, c := range clusters {
		if c.SpecJSON == "" {
			continue
		}
		var spec types.ProductSpec
		if err := json.Unmarshal([]byte(c.SpecJSON), &spec); err != nil {
			log.Printf("[api] warning: invalid spec JSON for cluster %d: %v", c.ID, err)
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
