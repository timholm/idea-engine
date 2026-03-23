package discover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/timholm/idea-engine/internal/types"
)

func TestScoreCandidate(t *testing.T) {
	tests := []struct {
		name     string
		paper    types.ArchivePaper
		pub      time.Time
		minScore float64
		maxScore float64
	}{
		{
			name: "recent CS.SE paper with citations",
			paper: types.ArchivePaper{
				Title:      "A Novel Framework for Code Generation",
				Categories: []string{"cs.SE"},
				CitedBy:    50,
			},
			pub:      time.Now().AddDate(0, 0, -3),
			minScore: 30, // recency + category + novelty + product keywords
			maxScore: 100,
		},
		{
			name: "old paper with no citations",
			paper: types.ArchivePaper{
				Title:      "Some Study",
				Categories: []string{"cs.CV"},
			},
			pub:      time.Now().AddDate(0, 0, -25),
			minScore: 0,
			maxScore: 30,
		},
		{
			name: "high citation velocity",
			paper: types.ArchivePaper{
				Title:      "Efficient Agent Pipeline",
				Categories: []string{"cs.AI", "cs.CL"},
				CitedBy:    100,
			},
			pub:      time.Now().AddDate(0, 0, -5),
			minScore: 40,
			maxScore: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreCandidate(tt.paper, tt.pub)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("score = %.1f, want between %.1f and %.1f", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestHasRelevantCategory(t *testing.T) {
	tests := []struct {
		cats []string
		want bool
	}{
		{[]string{"cs.AI"}, true},
		{[]string{"cs.CL", "cs.LG"}, true},
		{[]string{"math.CO"}, false},
		{[]string{"physics.hep-th"}, false},
		{[]string{"stat.ML"}, true},
		{nil, false},
	}

	for _, tt := range tests {
		got := hasRelevantCategory(tt.cats)
		if got != tt.want {
			t.Errorf("hasRelevantCategory(%v) = %v, want %v", tt.cats, got, tt.want)
		}
	}
}

func TestFilterAndScore(t *testing.T) {
	now := time.Now()
	papers := []types.ArchivePaper{
		{
			ArxivID:     "2603.00001",
			Title:       "Recent AI Agent Framework",
			Categories:  []string{"cs.AI"},
			Published:   now.AddDate(0, 0, -5).Format(time.RFC3339),
			HasFullText: true,
			CitedBy:     10,
		},
		{
			ArxivID:     "2603.00002",
			Title:       "Old Paper from Last Year",
			Categories:  []string{"cs.AI"},
			Published:   now.AddDate(-1, 0, 0).Format(time.RFC3339),
			HasFullText: true,
		},
		{
			ArxivID:     "2603.00003",
			Title:       "No Full Text Available",
			Categories:  []string{"cs.AI"},
			Published:   now.AddDate(0, 0, -2).Format(time.RFC3339),
			HasFullText: false,
		},
		{
			ArxivID:     "2603.00004",
			Title:       "Irrelevant Category",
			Categories:  []string{"math.CO"},
			Published:   now.AddDate(0, 0, -2).Format(time.RFC3339),
			HasFullText: true,
		},
	}

	d := &Discoverer{}
	candidates := d.filterAndScore(papers)

	// Should only include the first paper (recent, has full text, relevant category)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ArxivID != "2603.00001" {
		t.Errorf("expected arxiv ID 2603.00001, got %s", candidates[0].ArxivID)
	}
}

func TestDiscoverer_FetchPapers(t *testing.T) {
	papers := []types.ArchivePaper{
		{ArxivID: "2603.00001", Title: "Paper 1", HasFullText: true},
		{ArxivID: "2603.00002", Title: "Paper 2", HasFullText: true},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(papers)
	}))
	defer server.Close()

	d := &Discoverer{
		client: server.Client(),
	}

	result, err := d.fetchPapers(server.URL + "/papers/recent?cat=cs.AI&days=7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 papers, got %d", len(result))
	}
	if result[0].ArxivID != "2603.00001" {
		t.Errorf("first paper ArxivID = %s, want 2603.00001", result[0].ArxivID)
	}
}

func TestDiscoverer_FetchPapers_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	d := &Discoverer{
		client: server.Client(),
	}

	_, err := d.fetchPapers(server.URL + "/papers/recent")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}
