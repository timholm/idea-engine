// Package types defines the shared data structures used across idea-engine.
package types

import "time"

// PaperCluster represents 7 diverse papers from the same problem space, ready for fusion.
type PaperCluster struct {
	ID           int       `json:"id" db:"id"`
	ProblemSpace string    `json:"problem_space" db:"problem_space"`
	Papers       []ArchivePaper `json:"papers"`          // exactly 7 papers, each with a different technique
	PaperIDs     []string  `json:"paper_ids" db:"paper_ids"` // JSON array of 7 arxiv IDs
	Score        float64   `json:"score" db:"score"`    // commercial potential of this problem space
	Status       string    `json:"status" db:"status"`  // pending, researching, synthesized, delivered, skipped
	ResearchJSON string    `json:"research_json,omitempty" db:"research_json"`
	SpecJSON     string    `json:"spec_json,omitempty" db:"spec_json"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty" db:"delivered_at"`
}

// TechniqueSummary holds one paper's key technique for the fusion prompt.
type TechniqueSummary struct {
	ArxivID      string `json:"arxiv_id"`
	Title        string `json:"title"`
	Abstract     string `json:"abstract"`
	KeyTechnique string `json:"key_technique"` // 1 sentence: what's novel in this paper
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

// FusionResearchContext bundles all gathered research for a paper cluster.
type FusionResearchContext struct {
	Cluster    PaperCluster       `json:"cluster"`
	Techniques []TechniqueSummary `json:"techniques"` // 7 technique summaries, one per paper
	Repos      []RepoSummary      `json:"repos"`      // 7 GitHub repos related to the problem space
}

// ProductSpec is the final output: a buildable FUSION product specification for the factory.
type ProductSpec struct {
	Name           string            `json:"name"`
	Problem        string            `json:"problem"`
	Solution       string            `json:"solution"`
	Language       string            `json:"language"`
	Files          []string          `json:"files"`
	EstimatedLines int               `json:"estimated_lines"`
	TechniqueMap   map[string]string `json:"technique_map"`   // paper_arxiv_id -> how its technique is used
	SourcePapers   []string          `json:"source_papers"`   // 7 arxiv IDs
	SourceRepos    []string          `json:"source_repos"`    // 7 github URLs
	ProblemSpace   string            `json:"problem_space"`   // the fusion problem space
	MarketAnalysis string            `json:"market_analysis"` // who pays, why

	// Commercial viability fields
	BuyerPersona   string   `json:"buyer_persona"`    // exact buyer: job title, company size, industry
	PricePoint     string   `json:"price_point"`      // e.g. "$49/mo", "$499/yr enterprise"
	CompetingTools []string `json:"competing_tools"`   // existing solutions this replaces

	// Technical depth fields
	CoreAlgorithm string `json:"core_algorithm"` // the FUSION technique combining all 7
	APIDesign     string `json:"api_design"`     // key endpoints, CLI commands, or library API

	// Differentiation fields
	Differentiation string `json:"differentiation"` // why the fusion beats individual techniques
	Moat            string `json:"moat"`             // combinatorial advantage

	// Scope
	V1Scope string `json:"v1_scope"` // what ships in v1, buildable in one session

	// Factory self-improvement fields
	FactoryIntegration string `json:"factory_integration"` // which pipeline component this improves
	DeploymentTarget   string `json:"deployment_target"`   // which K8s service this replaces or augments
}

// ArchivePaper is the shape returned by the arxiv-archive HTTP API.
type ArchivePaper struct {
	ArxivID     string `json:"arxiv_id"`
	Title       string `json:"title"`
	Abstract    string `json:"abstract"`
	Authors     string `json:"authors"`     // comma-separated string
	Categories  string `json:"categories"`  // space-separated string
	Published   string `json:"published"`
	Updated     string `json:"updated,omitempty"`
	HasFullText bool   `json:"has_full_text"`
	FullText    string `json:"full_text,omitempty"`
	FetchedAt   string `json:"fetched_at,omitempty"`
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
	TotalClusters int     `json:"total_clusters"`
	Pending       int     `json:"pending"`
	Researching   int     `json:"researching"`
	Synthesized   int     `json:"synthesized"`
	Delivered     int     `json:"delivered"`
	Skipped       int     `json:"skipped"`
	ShippedIdeas  int     `json:"shipped_ideas"`
	AvgScore      float64 `json:"avg_score"`
}
