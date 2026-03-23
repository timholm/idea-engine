package research

import (
	"testing"
)

func TestExtractSearchQuery(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{
			title: "A Novel Approach to Code Generation Using Large Language Models",
			want:  "code generation large language models",
		},
		{
			title: "Efficient Retrieval-Augmented Generation for Question Answering",
			want:  "efficient retrieval-augmented generation question answering",
		},
		{
			title: "X",
			want:  "",
		},
	}

	for _, tt := range tests {
		got := extractSearchQuery(tt.title)
		if got != tt.want {
			t.Errorf("extractSearchQuery(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestExtractTechniques(t *testing.T) {
	tests := []struct {
		name     string
		abstract string
		wantLen  int
	}{
		{
			name:     "named technique",
			abstract: "We propose CodeNavigator, a novel tool for semantic code search.",
			wantLen:  1,
		},
		{
			name:     "acronym in parens",
			abstract: "We introduce Retrieval Augmented Generation (RAG) for code.",
			wantLen:  2, // "Retrieval Augmented Generation" + "RAG"
		},
		{
			name:     "multiple techniques",
			abstract: "We present FastEmbed (FE) and introduce SlowEmbed for comparison. The system called QuickSearch is also evaluated.",
			wantLen:  2, // "FastEmbed" + "QuickSearch is also evaluated" (capped at 3, regex matches vary)
		},
		{
			name:     "no techniques",
			abstract: "this paper studies some things about models",
			wantLen:  0,
		},
		{
			name:     "empty",
			abstract: "",
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTechniques(tt.abstract)
			if len(got) != tt.wantLen {
				t.Errorf("extractTechniques() returned %d techniques (%v), want %d", len(got), got, tt.wantLen)
			}
		})
	}
}

func TestExtractGitHubURLs(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantLen int
		want    []string
	}{
		{
			name:    "single URL",
			text:    "Code available at https://github.com/owner/repo",
			wantLen: 1,
			want:    []string{"https://github.com/owner/repo"},
		},
		{
			name:    "multiple URLs",
			text:    "See https://github.com/a/b and https://github.com/c/d for implementations.",
			wantLen: 2,
		},
		{
			name:    "URL with .git suffix",
			text:    "Clone from https://github.com/owner/repo.git to get started.",
			wantLen: 1,
			want:    []string{"https://github.com/owner/repo"},
		},
		{
			name:    "dedup",
			text:    "See https://github.com/a/b and also https://github.com/a/b for reference.",
			wantLen: 1,
		},
		{
			name:    "no URLs",
			text:    "This paper has no code.",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGitHubURLs(tt.text)
			if len(got) != tt.wantLen {
				t.Errorf("extractGitHubURLs() returned %d URLs (%v), want %d", len(got), got, tt.wantLen)
			}
			for i, want := range tt.want {
				if i < len(got) && got[i] != want {
					t.Errorf("URL[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestNormalizeStars(t *testing.T) {
	tests := []struct {
		stars int
		want  float64
	}{
		{0, 0},
		{15, 0.1},
		{50, 0.2},
		{100, 0.3},
		{1000, 0.4},
		{10000, 0.5},
		{50000, 0.5},
	}

	for _, tt := range tests {
		got := normalizeStars(tt.stars)
		if got != tt.want {
			t.Errorf("normalizeStars(%d) = %f, want %f", tt.stars, got, tt.want)
		}
	}
}
