package synthesize

import (
	"strings"
	"testing"

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
	ctx := &types.FusionResearchContext{
		Cluster: types.PaperCluster{
			ProblemSpace: "LLM inference optimization",
			PaperIDs: []string{
				"2603.00001", "2603.00002", "2603.00003",
				"2603.00004", "2603.00005", "2603.00006", "2603.00007",
			},
		},
		Techniques: []types.TechniqueSummary{
			{ArxivID: "2603.00001", Title: "Speculative Decoding", KeyTechnique: "Draft-verify pattern", Abstract: "We propose speculative decoding."},
			{ArxivID: "2603.00002", Title: "KV Cache Compression", KeyTechnique: "Cache compression", Abstract: "We present KV cache compression."},
		},
		Repos: []types.RepoSummary{
			{URL: "https://github.com/a/b", Name: "a/b", Stars: 500, Language: "Go", Description: "Inference tool"},
		},
	}

	prompt := buildPrompt(ctx)

	// Check that key fusion sections are present
	checks := []string{
		"FUSION",
		"LLM inference optimization",
		"7 Techniques To Fuse",
		"Speculative Decoding",
		"KV Cache Compression",
		"technique_map",
		"ALL 7 papers",
		"factory_integration",
		"deployment_target",
		"factory pipeline",
		"a/b",
		"500",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing expected content: %q", check)
		}
	}
}
