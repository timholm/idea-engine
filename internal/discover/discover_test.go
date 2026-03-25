package discover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/timholm/idea-engine/internal/types"
)

func TestExtractTechniqueKeywords(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		abstract string
		wantMin  int // minimum expected keywords
	}{
		{
			name:     "inference paper",
			title:    "Speculative Decoding with KV Cache Compression for LLM Inference",
			abstract: "We propose a method combining speculative decoding with quantization for efficient batching.",
			wantMin:  3, // speculative, quantization, batching at minimum
		},
		{
			name:     "code generation paper",
			title:    "Tree-based Code Generation with Attention",
			abstract: "Our approach uses transformer attention for code generation with program synthesis.",
			wantMin:  2,
		},
		{
			name:     "empty",
			title:    "",
			abstract: "",
			wantMin:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kw := extractTechniqueKeywords(tt.title, tt.abstract)
			if len(kw) < tt.wantMin {
				t.Errorf("extractTechniqueKeywords() returned %d keywords, want at least %d (got: %v)",
					len(kw), tt.wantMin, kw)
			}
		})
	}
}

func TestPickDiversePapers(t *testing.T) {
	// Create papers with overlapping and non-overlapping keywords
	papers := []paperWithKeywords{
		{paper: types.ArchivePaper{ArxivID: "1"}, keywords: map[string]bool{"attention": true, "transformer": true}},
		{paper: types.ArchivePaper{ArxivID: "2"}, keywords: map[string]bool{"attention": true, "pruning": true}}, // overlaps with 1
		{paper: types.ArchivePaper{ArxivID: "3"}, keywords: map[string]bool{"quantization": true, "compression": true}},
		{paper: types.ArchivePaper{ArxivID: "4"}, keywords: map[string]bool{"retrieval": true, "embedding": true}},
		{paper: types.ArchivePaper{ArxivID: "5"}, keywords: map[string]bool{"agent": true, "planning": true}},
		{paper: types.ArchivePaper{ArxivID: "6"}, keywords: map[string]bool{"code generation": true, "testing": true}},
		{paper: types.ArchivePaper{ArxivID: "7"}, keywords: map[string]bool{"distillation": true, "sparse": true}},
		{paper: types.ArchivePaper{ArxivID: "8"}, keywords: map[string]bool{"attention": true, "transformer": true}}, // duplicate of 1
	}

	selected := pickDiversePapers(papers, 7)

	if len(selected) != 7 {
		t.Fatalf("expected 7 papers, got %d", len(selected))
	}

	// Check no duplicates
	seen := make(map[string]bool)
	for _, p := range selected {
		if seen[p.paper.ArxivID] {
			t.Errorf("duplicate paper selected: %s", p.paper.ArxivID)
		}
		seen[p.paper.ArxivID] = true
	}
}

func TestScoreCluster(t *testing.T) {
	papers := []paperWithKeywords{
		{
			paper:    types.ArchivePaper{Published: time.Now().AddDate(0, 0, -3).Format(time.RFC3339)},
			keywords: map[string]bool{"attention": true, "transformer": true},
			category: "cs.SE",
		},
		{
			paper:    types.ArchivePaper{Published: time.Now().AddDate(0, 0, -5).Format(time.RFC3339)},
			keywords: map[string]bool{"quantization": true, "compression": true},
			category: "cs.CL",
		},
	}

	score := scoreCluster("LLM inference optimization", papers)
	if score <= 0 {
		t.Errorf("expected positive score, got %.1f", score)
	}
	if score > 100 {
		t.Errorf("score %.1f exceeds maximum 100", score)
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

func TestFilterPapers(t *testing.T) {
	now := time.Now()
	papers := []types.ArchivePaper{
		{
			ArxivID:    "2603.00001",
			Title:      "Recent AI Paper",
			Abstract:   "We study something important.",
			Categories: "cs.AI",
			Published:  now.AddDate(0, 0, -5).Format(time.RFC3339),
		},
		{
			ArxivID:    "2603.00002",
			Title:      "Old Paper",
			Abstract:   "Ancient work.",
			Categories: "cs.AI",
			Published:  now.AddDate(-1, 0, 0).Format(time.RFC3339),
		},
		{
			ArxivID:    "2603.00003",
			Title:      "No Abstract",
			Abstract:   "",
			Categories: "cs.AI",
			Published:  now.AddDate(0, 0, -2).Format(time.RFC3339),
		},
		{
			ArxivID:    "2603.00004",
			Title:      "Irrelevant Category",
			Abstract:   "Some work.",
			Categories: "math.CO",
			Published:  now.AddDate(0, 0, -2).Format(time.RFC3339),
		},
	}

	d := &Discoverer{}
	filtered := d.filterPapers(papers)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered paper, got %d", len(filtered))
	}
	if filtered[0].ArxivID != "2603.00001" {
		t.Errorf("expected arxiv ID 2603.00001, got %s", filtered[0].ArxivID)
	}
}
