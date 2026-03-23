// Package types defines the shared data structures used across idea-engine.
package types

import "time"

// Candidate represents a paper being evaluated for product potential.
type Candidate struct {
	ID           int       `json:"id" db:"id"`
	ArxivID      string    `json:"arxiv_id" db:"arxiv_id"`
	Title        string    `json:"title" db:"title"`
	Abstract     string    `json:"abstract,omitempty"`
	FullText     string    `json:"full_text,omitempty"`
	Categories   []string  `json:"categories,omitempty"`
	Published    time.Time `json:"published"`
	Score        float64   `json:"score" db:"score"`
	Status       string    `json:"status" db:"status"` // pending, researching, synthesized, delivered, skipped
	ResearchJSON string    `json:"research_json,omitempty" db:"research_json"`
	SpecJSON     string    `json:"spec_json,omitempty" db:"spec_json"`
	DiscoveredAt time.Time `json:"discovered_at" db:"discovered_at"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty" db:"delivered_at"`
}

// PaperSummary holds a related paper's key information for the research context.
type PaperSummary struct {
	ArxivID     string  `json:"arxiv_id"`
	Title       string  `json:"title"`
	Abstract    string  `json:"abstract"`
	KeyFindings string  `json:"key_findings"` // extracted by LLM or from abstract
	Relevance   float64 `json:"relevance"`
}

// RepoSummary holds a related GitHub repo's key information for the research context.
type RepoSummary struct {
	URL         string   `json:"url"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Stars       int      `json:"stars"`
	Language    string   `json:"language"`
	README      string   `json:"readme"`
	FileTree    []string `json:"file_tree"`
	Relevance   float64  `json:"relevance"`
}

// ResearchContext bundles all gathered research for a single candidate.
type ResearchContext struct {
	Candidate Candidate      `json:"candidate"`
	Papers    []PaperSummary `json:"papers"` // 7 related papers
	Repos     []RepoSummary  `json:"repos"`  // 7 GitHub repos
}

// ProductSpec is the final output: a buildable product specification for the factory.
type ProductSpec struct {
	Name           string   `json:"name"`
	Problem        string   `json:"problem"`
	Solution       string   `json:"solution"`
	Language       string   `json:"language"`
	Files          []string `json:"files"`
	EstimatedLines int      `json:"estimated_lines"`
	SourcePapers   []string `json:"source_papers"`  // 7 arxiv IDs
	SourceRepos    []string `json:"source_repos"`    // 7 github URLs
	SourceURL      string   `json:"source_url"`      // primary paper
	MarketAnalysis string   `json:"market_analysis"` // who pays, why
}

// ArchivePaper is the shape returned by the arxiv-archive HTTP API.
type ArchivePaper struct {
	ArxivID     string   `json:"arxiv_id"`
	Title       string   `json:"title"`
	Abstract    string   `json:"abstract"`
	Authors     []string `json:"authors"`
	Categories  []string `json:"categories"`
	Published   string   `json:"published"`
	HasFullText bool     `json:"has_full_text"`
	FullText    string   `json:"full_text,omitempty"`
	CitedBy     int      `json:"cited_by,omitempty"`
}

// GitHubSearchResult represents a single GitHub search API result.
type GitHubSearchResult struct {
	TotalCount int          `json:"total_count"`
	Items      []GitHubRepo `json:"items"`
}

// GitHubRepo is a single repo from the GitHub search API.
type GitHubRepo struct {
	FullName    string `json:"full_name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`
	Archived    bool   `json:"archived"`
	PushedAt    string `json:"pushed_at"`
}

// GitHubContent is one entry from the GitHub contents API (file tree).
type GitHubContent struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "dir"
}

// LLMRequest is the request body for llm-router's chat completions endpoint.
type LLMRequest struct {
	Model    string       `json:"model"`
	Messages []LLMMessage `json:"messages"`
}

// LLMMessage is a single message in an LLM conversation.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponse is the response from llm-router's chat completions endpoint.
type LLMResponse struct {
	Choices []LLMChoice `json:"choices"`
}

// LLMChoice is one completion choice.
type LLMChoice struct {
	Message LLMMessage `json:"message"`
}

// Stats holds aggregate pipeline statistics.
type Stats struct {
	TotalCandidates int     `json:"total_candidates"`
	Pending         int     `json:"pending"`
	Researching     int     `json:"researching"`
	Synthesized     int     `json:"synthesized"`
	Delivered       int     `json:"delivered"`
	Skipped         int     `json:"skipped"`
	ShippedIdeas    int     `json:"shipped_ideas"`
	AvgScore        float64 `json:"avg_score"`
}
