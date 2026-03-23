package synthesize

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/types"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "raw JSON",
			input: `{"name": "test"}`,
			want:  `{"name": "test"}`,
		},
		{
			name:  "markdown code block",
			input: "Here's the spec:\n```json\n{\"name\": \"test\"}\n```\nDone.",
			want:  `{"name": "test"}`,
		},
		{
			name:  "generic code block",
			input: "```\n{\"name\": \"test\"}\n```",
			want:  `{"name": "test"}`,
		},
		{
			name:  "nested braces",
			input: `{"name": "test", "nested": {"key": "val"}}`,
			want:  `{"name": "test", "nested": {"key": "val"}}`,
		},
		{
			name:  "no JSON",
			input: "Just some text with no JSON at all",
			want:  "",
		},
		{
			name:  "text before JSON",
			input: `Sure, here's the product spec: {"name": "test", "problem": "none"}`,
			want:  `{"name": "test", "problem": "none"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	ctx := &types.ResearchContext{
		Candidate: types.Candidate{
			ArxivID:  "2603.16514",
			Title:    "Test Paper: A Novel Approach",
			Abstract: "We study X and propose Y.",
			FullText: "Full text of the paper goes here.",
		},
		Papers: []types.PaperSummary{
			{ArxivID: "2603.00001", Title: "Related 1", Abstract: "Abstract 1", Relevance: 0.95},
			{ArxivID: "2603.00002", Title: "Related 2", Abstract: "Abstract 2", Relevance: 0.90},
		},
		Repos: []types.RepoSummary{
			{URL: "https://github.com/a/b", Name: "a/b", Stars: 500, Language: "Go", Description: "Desc"},
		},
	}

	prompt := buildPrompt(ctx)

	// Check that key sections are present
	checks := []string{
		"Test Paper: A Novel Approach",
		"2603.16514",
		"Full text of the paper goes here.",
		"Related 1",
		"Related 2",
		"a/b",
		"500",
		"Output ONLY the JSON object",
		"market_analysis",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing expected content: %q", check)
		}
	}
}

func TestSynthesizer_CallLLM(t *testing.T) {
	spec := types.ProductSpec{
		Name:           "test-product",
		Problem:        "Test problem",
		Solution:       "Test solution",
		Language:       "Go",
		Files:          []string{"main.go"},
		EstimatedLines: 1000,
		MarketAnalysis: "Test market",
	}

	specJSON, _ := json.Marshal(spec)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		resp := types.LLMResponse{
			Choices: []types.LLMChoice{
				{Message: types.LLMMessage{
					Role:    "assistant",
					Content: string(specJSON),
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := &Synthesizer{
		cfg:    &config.Config{LLMRouterURL: server.URL},
		client: server.Client(),
	}

	result, err := s.callLLM("test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != "test-product" {
		t.Errorf("Name = %q, want %q", result.Name, "test-product")
	}
	if result.Language != "Go" {
		t.Errorf("Language = %q, want %q", result.Language, "Go")
	}
}

func TestSynthesizer_CallLLM_MarkdownWrapped(t *testing.T) {
	spec := types.ProductSpec{
		Name:    "wrapped-product",
		Problem: "Test",
		Solution: "Test",
	}
	specJSON, _ := json.Marshal(spec)
	wrapped := "Here's the spec:\n```json\n" + string(specJSON) + "\n```"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.LLMResponse{
			Choices: []types.LLMChoice{
				{Message: types.LLMMessage{Role: "assistant", Content: wrapped}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := &Synthesizer{
		cfg:    &config.Config{LLMRouterURL: server.URL},
		client: server.Client(),
	}

	result, err := s.callLLM("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "wrapped-product" {
		t.Errorf("Name = %q, want %q", result.Name, "wrapped-product")
	}
}

func TestSynthesizer_CallLLM_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.LLMResponse{
			Choices: []types.LLMChoice{
				{Message: types.LLMMessage{Role: "assistant", Content: "I can't generate a spec for this."}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := &Synthesizer{
		cfg:    &config.Config{LLMRouterURL: server.URL},
		client: server.Client(),
	}

	_, err := s.callLLM("test")
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}
