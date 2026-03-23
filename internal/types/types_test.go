package types

import (
	"encoding/json"
	"testing"
)

func TestProductSpec_JSON(t *testing.T) {
	spec := ProductSpec{
		Name:           "code-navigator",
		Problem:        "Developers waste time navigating large codebases",
		Solution:       "Semantic code search using paper embeddings",
		Language:       "Go",
		Files:          []string{"main.go", "internal/search/search.go"},
		EstimatedLines: 3000,
		SourcePapers:   []string{"2603.16514", "2603.12345"},
		SourceRepos:    []string{"https://github.com/example/repo"},
		SourceURL:      "https://arxiv.org/abs/2603.16514",
		MarketAnalysis: "Enterprise dev teams pay $50/seat/month for faster code navigation",
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProductSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Name != spec.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, spec.Name)
	}
	if decoded.EstimatedLines != spec.EstimatedLines {
		t.Errorf("EstimatedLines = %d, want %d", decoded.EstimatedLines, spec.EstimatedLines)
	}
	if len(decoded.SourcePapers) != 2 {
		t.Errorf("SourcePapers length = %d, want 2", len(decoded.SourcePapers))
	}
	if len(decoded.Files) != 2 {
		t.Errorf("Files length = %d, want 2", len(decoded.Files))
	}
}

func TestResearchContext_JSON(t *testing.T) {
	ctx := ResearchContext{
		Candidate: Candidate{
			ArxivID: "2603.16514",
			Title:   "Test Paper",
		},
		Papers: []PaperSummary{
			{ArxivID: "2603.11111", Title: "Related 1", Relevance: 0.95},
		},
		Repos: []RepoSummary{
			{URL: "https://github.com/test/repo", Name: "test/repo", Stars: 100, Relevance: 0.8},
		},
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ResearchContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Candidate.ArxivID != "2603.16514" {
		t.Errorf("Candidate.ArxivID = %q, want %q", decoded.Candidate.ArxivID, "2603.16514")
	}
	if len(decoded.Papers) != 1 {
		t.Errorf("Papers length = %d, want 1", len(decoded.Papers))
	}
	if len(decoded.Repos) != 1 {
		t.Errorf("Repos length = %d, want 1", len(decoded.Repos))
	}
}

func TestStats_JSON(t *testing.T) {
	stats := Stats{
		TotalCandidates: 100,
		Pending:         30,
		Delivered:       50,
		AvgScore:        72.5,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Stats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalCandidates != 100 {
		t.Errorf("TotalCandidates = %d, want 100", decoded.TotalCandidates)
	}
	if decoded.AvgScore != 72.5 {
		t.Errorf("AvgScore = %f, want 72.5", decoded.AvgScore)
	}
}
