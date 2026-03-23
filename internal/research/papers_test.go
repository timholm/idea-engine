package research

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/types"
)

func TestExtractKeyFindings(t *testing.T) {
	tests := []struct {
		name     string
		abstract string
		want     string
	}{
		{
			name:     "empty",
			abstract: "",
			want:     "",
		},
		{
			name:     "with propose signal",
			abstract: "Language models are powerful. We propose a novel technique for code generation. Results are promising.",
			want:     "We propose a novel technique for code generation.",
		},
		{
			name:     "with achieves signal",
			abstract: "We study transformers. Our model achieves state-of-the-art on three benchmarks. More analysis is needed.",
			want:     "Our model achieves state-of-the-art on three benchmarks.",
		},
		{
			name:     "fallback to last sentence",
			abstract: "This paper studies X. We analyze Y. The gap remains open",
			want:     "The gap remains open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeyFindings(tt.abstract)
			if got != tt.want {
				t.Errorf("extractKeyFindings() = %q, want %q", got, tt.want)
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

func TestPaperResearcher_FindRelatedPapers(t *testing.T) {
	mux := http.NewServeMux()

	// Similar papers endpoint
	mux.HandleFunc("/papers/similar/", func(w http.ResponseWriter, r *http.Request) {
		papers := []types.ArchivePaper{
			{ArxivID: "2603.00001", Title: "Similar 1", Abstract: "We propose a method"},
			{ArxivID: "2603.00002", Title: "Similar 2", Abstract: "Results show improvement"},
			{ArxivID: "2603.00003", Title: "Similar 3", Abstract: "Novel approach"},
		}
		json.NewEncoder(w).Encode(papers)
	})

	// Cited-by endpoint
	mux.HandleFunc("/papers/2603.16514/cited-by", func(w http.ResponseWriter, r *http.Request) {
		papers := []types.ArchivePaper{
			{ArxivID: "2603.00004", Title: "Citer 1", Abstract: "Building on prior work"},
			{ArxivID: "2603.00005", Title: "Citer 2", Abstract: "Extension of results"},
		}
		json.NewEncoder(w).Encode(papers)
	})

	// Refs endpoint
	mux.HandleFunc("/papers/2603.16514/refs", func(w http.ResponseWriter, r *http.Request) {
		papers := []types.ArchivePaper{
			{ArxivID: "2603.00006", Title: "Ref 1", Abstract: "Foundation work"},
			{ArxivID: "2603.00007", Title: "Ref 2", Abstract: "Key technique"},
			{ArxivID: "2603.00008", Title: "Ref 3", Abstract: "Another foundation"},
		}
		json.NewEncoder(w).Encode(papers)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	pr := &PaperResearcher{
		cfg:    &config.Config{ArchiveURL: server.URL},
		client: server.Client(),
	}

	papers, err := pr.FindRelatedPapers("2603.16514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(papers) != 7 {
		t.Fatalf("expected 7 papers, got %d", len(papers))
	}

	// Check that they're sorted by relevance
	for i := 1; i < len(papers); i++ {
		if papers[i].Relevance > papers[i-1].Relevance {
			t.Errorf("papers not sorted by relevance: [%d]=%.2f > [%d]=%.2f",
				i, papers[i].Relevance, i-1, papers[i-1].Relevance)
		}
	}

	// All should have key findings extracted
	for _, p := range papers {
		if p.KeyFindings == "" {
			t.Errorf("paper %s has empty KeyFindings", p.ArxivID)
		}
	}
}
