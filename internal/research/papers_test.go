package research

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/types"
)

func TestExtractKeyTechnique(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		abstract string
		want     string
	}{
		{
			name:     "empty",
			title:    "Some Paper",
			abstract: "",
			want:     "Some Paper",
		},
		{
			name:     "with propose signal",
			title:    "Test",
			abstract: "Language models are powerful. We propose a novel technique for code generation. Results are promising.",
			want:     "We propose a novel technique for code generation.",
		},
		{
			name:     "with achieves signal",
			title:    "Test",
			abstract: "We study transformers. Our model achieves state-of-the-art on three benchmarks. More analysis is needed.",
			want:     "Our model achieves state-of-the-art on three benchmarks.",
		},
		{
			name:     "fallback to first sentence",
			title:    "Test",
			abstract: "This paper studies X. We analyze Y. The gap remains open",
			want:     "This paper studies X.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeyTechnique(tt.title, tt.abstract)
			if got != tt.want {
				t.Errorf("extractKeyTechnique() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPaperResearcher_FetchFullPaper(t *testing.T) {
	paper := types.ArchivePaper{
		ArxivID:  "2603.16514",
		Title:    "Test Paper",
		Abstract: "Test abstract",
		FullText: "Full text here",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/papers/2603.16514" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(paper)
	}))
	defer server.Close()

	pr := &PaperResearcher{
		cfg:    &config.Config{ArchiveURL: server.URL},
		client: server.Client(),
	}

	result, err := pr.FetchFullPaper("2603.16514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ArxivID != "2603.16514" {
		t.Errorf("ArxivID = %s, want 2603.16514", result.ArxivID)
	}
	if result.FullText != "Full text here" {
		t.Errorf("FullText = %q, want %q", result.FullText, "Full text here")
	}
}

func TestPaperResearcher_ExtractTechniques(t *testing.T) {
	papers := []types.ArchivePaper{
		{ArxivID: "2603.00001", Title: "Speculative Decoding", Abstract: "We propose a draft-verify approach."},
		{ArxivID: "2603.00002", Title: "KV Cache Compression", Abstract: "We present a method to compress key-value caches."},
		{ArxivID: "2603.00003", Title: "Continuous Batching", Abstract: "Our approach enables continuous batching for throughput."},
	}

	pr := &PaperResearcher{
		cfg:    &config.Config{ArchiveURL: "http://unused"},
		client: http.DefaultClient,
	}

	techniques, err := pr.ExtractTechniques(papers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(techniques) != 3 {
		t.Fatalf("expected 3 techniques, got %d", len(techniques))
	}

	// Each should have a key technique extracted
	for _, tech := range techniques {
		if tech.KeyTechnique == "" {
			t.Errorf("paper %s has empty KeyTechnique", tech.ArxivID)
		}
		if tech.ArxivID == "" {
			t.Error("technique has empty ArxivID")
		}
	}

	// First paper's technique should mention "propose" since that's in the abstract
	if techniques[0].KeyTechnique != "We propose a draft-verify approach." {
		t.Errorf("technique[0] = %q, want %q", techniques[0].KeyTechnique, "We propose a draft-verify approach.")
	}
}
