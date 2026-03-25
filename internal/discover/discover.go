// Package discover queries arxiv-archive to find paper clusters for fusion products.
// Instead of individual candidates, it finds PROBLEM SPACES and groups 7 diverse papers
// into each cluster — papers that tackle the same problem with different techniques.
package discover

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// problemSpaces are high-signal problem domains with commercial potential.
// Each maps to a search query for arxiv-archive.
var problemSpaces = []string{
	"LLM inference optimization",
	"code generation and synthesis",
	"retrieval augmented generation",
	"AI agent orchestration",
	"code security and vulnerability detection",
	"automated testing and fuzzing",
	"embeddings and vector search",
	"model compression and quantization",
	"prompt engineering and optimization",
	"data pipeline and ETL automation",
}

// categories to query for recent papers.
var categories = []string{"cs.AI", "cs.CL", "cs.LG", "cs.SE", "cs.CV", "stat.ML"}

const clusterSize = 7 // exactly 7 papers per fusion cluster

// Discoverer finds paper clusters from the arxiv-archive API.
type Discoverer struct {
	cfg    *config.Config
	db     *db.DB
	client *http.Client
}

// New creates a Discoverer.
func New(cfg *config.Config, database *db.DB) *Discoverer {
	return &Discoverer{
		cfg: cfg,
		db:  database,
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// Run discovers paper clusters: fetches recent papers, groups them by problem space,
// picks the 7 most diverse papers per space, and returns scored clusters.
func (d *Discoverer) Run() ([]types.PaperCluster, error) {
	seen := make(map[string]bool)
	var allPapers []types.ArchivePaper

	// Step 1: Recent papers from each category (last 7 days)
	for _, cat := range categories {
		papers, err := d.fetchRecent(cat, 7)
		if err != nil {
			log.Printf("[discover] warning: failed to fetch recent %s: %v", cat, err)
			continue
		}
		for _, p := range papers {
			if !seen[p.ArxivID] {
				seen[p.ArxivID] = true
				allPapers = append(allPapers, p)
			}
		}
	}

	// Step 2: Problem space searches — find papers for each target domain
	for _, space := range problemSpaces {
		papers, err := d.searchPapers(space)
		if err != nil {
			log.Printf("[discover] warning: failed to search '%s': %v", space, err)
			continue
		}
		for _, p := range papers {
			if !seen[p.ArxivID] {
				seen[p.ArxivID] = true
				allPapers = append(allPapers, p)
			}
		}
	}

	log.Printf("[discover] found %d unique papers from archive", len(allPapers))

	// Step 3: Filter to recent, relevant papers
	filtered := d.filterPapers(allPapers)
	log.Printf("[discover] %d papers pass quality filters", len(filtered))

	if len(filtered) < clusterSize {
		return nil, fmt.Errorf("only %d papers pass filters, need at least %d for a cluster", len(filtered), clusterSize)
	}

	// Step 4: Cluster papers into problem spaces by keyword similarity
	clusters := d.buildClusters(filtered)

	// Step 5: Take top N clusters
	limit := d.cfg.CandidatesPerRun
	if limit > len(clusters) {
		limit = len(clusters)
	}
	clusters = clusters[:limit]

	// Step 6: Persist clusters to database
	for i := range clusters {
		id, err := d.db.InsertCluster(clusters[i].ProblemSpace, clusters[i].PaperIDs, clusters[i].Score)
		if err != nil {
			log.Printf("[discover] warning: failed to insert cluster '%s': %v", clusters[i].ProblemSpace, err)
			continue
		}
		clusters[i].ID = id
	}

	log.Printf("[discover] built %d fusion clusters for research", len(clusters))
	return clusters, nil
}

func (d *Discoverer) fetchRecent(category string, days int) ([]types.ArchivePaper, error) {
	u := fmt.Sprintf("%s/papers/recent?cat=%s&days=%d", d.cfg.ArchiveURL, url.QueryEscape(category), days)
	return d.fetchPapers(u)
}

func (d *Discoverer) searchPapers(query string) ([]types.ArchivePaper, error) {
	u := fmt.Sprintf("%s/papers/search?q=%s", d.cfg.ArchiveURL, url.QueryEscape(query))
	return d.fetchPapers(u)
}

func (d *Discoverer) fetchPapers(endpoint string) ([]types.ArchivePaper, error) {
	resp, err := d.client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s returned %d: %s", endpoint, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", endpoint, err)
	}

	// Try bare array first.
	var papers []types.ArchivePaper
	if err := json.Unmarshal(body, &papers); err == nil {
		return papers, nil
	}

	// Try wrapped format: {"papers": [...], "count": N, ...}
	var wrapped struct {
		Papers  []types.ArchivePaper `json:"papers"`
		Refs    []types.ArchivePaper `json:"refs"`
		CitedBy []types.ArchivePaper `json:"cited_by"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", endpoint, err)
	}
	if len(wrapped.Papers) > 0 {
		return wrapped.Papers, nil
	}
	if len(wrapped.Refs) > 0 {
		return wrapped.Refs, nil
	}
	if len(wrapped.CitedBy) > 0 {
		return wrapped.CitedBy, nil
	}
	return nil, nil
}

// filterPapers applies quality filters: recent, has abstract, relevant category.
func (d *Discoverer) filterPapers(papers []types.ArchivePaper) []types.ArchivePaper {
	cutoff := time.Now().AddDate(0, 0, -30)
	var result []types.ArchivePaper

	for _, p := range papers {
		pub, err := time.Parse(time.RFC3339, p.Published)
		if err != nil {
			pub, err = time.Parse("2006-01-02", p.Published)
			if err != nil {
				continue
			}
		}
		if pub.Before(cutoff) {
			continue
		}
		if p.Abstract == "" {
			continue
		}
		cats := strings.Fields(p.Categories)
		if !hasRelevantCategory(cats) {
			continue
		}
		result = append(result, p)
	}
	return result
}

// paperWithKeywords annotates a paper with extracted technique keywords.
type paperWithKeywords struct {
	paper    types.ArchivePaper
	keywords map[string]bool
	category string // primary category
}

// buildClusters groups papers into problem spaces and picks the 7 most diverse per space.
func (d *Discoverer) buildClusters(papers []types.ArchivePaper) []types.PaperCluster {
	// Step 1: Extract technique keywords from each paper title + abstract
	var annotated []paperWithKeywords
	for _, p := range papers {
		kw := extractTechniqueKeywords(p.Title, p.Abstract)
		cats := strings.Fields(p.Categories)
		primary := ""
		if len(cats) > 0 {
			primary = cats[0]
		}
		annotated = append(annotated, paperWithKeywords{
			paper:    p,
			keywords: kw,
			category: primary,
		})
	}

	// Step 2: Group papers by problem space
	// A paper belongs to a problem space if its title/abstract contains matching keywords
	spaceKeywords := map[string][]string{
		"LLM inference optimization":             {"inference", "decoding", "latency", "throughput", "serving", "kv cache", "quantization", "pruning", "batching", "speculative", "compression"},
		"code generation and synthesis":           {"code generation", "program synthesis", "code completion", "code repair", "code translation", "programming", "software engineering", "code edit"},
		"retrieval augmented generation":          {"retrieval", "rag", "knowledge base", "context window", "document", "embedding", "vector search", "semantic search"},
		"AI agent orchestration":                  {"agent", "multi-agent", "tool use", "planning", "reasoning", "chain of thought", "workflow", "orchestration", "function calling"},
		"code security and vulnerability":         {"vulnerability", "security", "malware", "exploit", "fuzzing", "static analysis", "bug detection", "code review", "patch"},
		"automated testing":                       {"testing", "test generation", "fuzzing", "mutation", "coverage", "verification", "validation", "debugging"},
		"embeddings and vector search":            {"embedding", "representation", "similarity", "clustering", "dimensionality", "contrastive", "vector", "dense retrieval"},
		"model compression and efficiency":        {"distillation", "pruning", "quantization", "compression", "efficient", "lightweight", "mobile", "edge", "small model"},
		"prompt engineering and optimization":     {"prompt", "instruction", "alignment", "tuning", "few-shot", "in-context learning", "chain of thought"},
		"data pipeline and ML ops":                {"pipeline", "data", "feature", "monitoring", "drift", "deployment", "mlops", "training", "preprocessing"},
	}

	type spaceGroup struct {
		name   string
		papers []paperWithKeywords
	}

	var groups []spaceGroup
	for space, keywords := range spaceKeywords {
		var matched []paperWithKeywords
		for _, pw := range annotated {
			titleAbstract := strings.ToLower(pw.paper.Title + " " + pw.paper.Abstract)
			matchCount := 0
			for _, kw := range keywords {
				if strings.Contains(titleAbstract, kw) {
					matchCount++
				}
			}
			if matchCount >= 2 { // must match at least 2 keywords for the space
				matched = append(matched, pw)
			}
		}
		if len(matched) >= clusterSize {
			groups = append(groups, spaceGroup{name: space, papers: matched})
		}
	}

	// Step 3: For each problem space, pick the 7 most DIVERSE papers
	var clusters []types.PaperCluster
	for _, group := range groups {
		diverse := pickDiversePapers(group.papers, clusterSize)
		if len(diverse) < clusterSize {
			continue
		}

		var paperIDs []string
		var clusterPapers []types.ArchivePaper
		for _, pw := range diverse {
			paperIDs = append(paperIDs, pw.paper.ArxivID)
			clusterPapers = append(clusterPapers, pw.paper)
		}

		score := scoreCluster(group.name, diverse)

		clusters = append(clusters, types.PaperCluster{
			ProblemSpace: group.name,
			Papers:       clusterPapers,
			PaperIDs:     paperIDs,
			Score:        score,
			Status:       "pending",
		})
	}

	// Sort clusters by score descending
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Score > clusters[j].Score
	})

	return clusters
}

// pickDiversePapers selects N papers with maximum keyword diversity.
// Uses a greedy algorithm: always pick the paper whose keywords overlap
// LEAST with the keywords already selected.
func pickDiversePapers(papers []paperWithKeywords, n int) []paperWithKeywords {
	if len(papers) <= n {
		return papers
	}

	// Start with the paper that has the most unique keywords (richest technique)
	best := 0
	bestCount := 0
	for i, pw := range papers {
		if len(pw.keywords) > bestCount {
			bestCount = len(pw.keywords)
			best = i
		}
	}

	selected := []paperWithKeywords{papers[best]}
	usedKeywords := make(map[string]bool)
	for kw := range papers[best].keywords {
		usedKeywords[kw] = true
	}
	used := map[int]bool{best: true}

	for len(selected) < n {
		bestIdx := -1
		bestNewKeywords := -1

		for i, pw := range papers {
			if used[i] {
				continue
			}
			// Count how many NEW keywords this paper brings
			newKW := 0
			for kw := range pw.keywords {
				if !usedKeywords[kw] {
					newKW++
				}
			}
			if newKW > bestNewKeywords {
				bestNewKeywords = newKW
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}

		selected = append(selected, papers[bestIdx])
		used[bestIdx] = true
		for kw := range papers[bestIdx].keywords {
			usedKeywords[kw] = true
		}
	}

	return selected
}

// extractTechniqueKeywords extracts technique-related keywords from title and abstract.
func extractTechniqueKeywords(title, abstract string) map[string]bool {
	text := strings.ToLower(title + " " + abstract)
	keywords := make(map[string]bool)

	// Technique-indicating words
	techniqueWords := []string{
		"attention", "transformer", "convolution", "diffusion", "reinforcement",
		"contrastive", "generative", "discriminative", "autoregressive", "masked",
		"retrieval", "embedding", "quantization", "pruning", "distillation",
		"fine-tuning", "pretraining", "few-shot", "zero-shot", "in-context",
		"agent", "planning", "reasoning", "chain-of-thought", "tool-use",
		"speculative", "batching", "caching", "streaming", "parallel",
		"graph", "tree", "sequence", "hierarchical", "recursive",
		"adversarial", "robust", "calibration", "uncertainty",
		"federated", "distributed", "compression", "sparse",
		"multimodal", "vision-language", "cross-modal",
		"code generation", "program synthesis", "code repair",
		"vulnerability", "fuzzing", "testing", "verification",
		"search", "ranking", "recommendation", "clustering",
		"tokenization", "parsing", "grammar", "syntax",
		"knowledge graph", "ontology", "schema",
		"alignment", "rlhf", "dpo", "preference",
		"mixture of experts", "moe", "routing",
		"flash attention", "linear attention", "sparse attention",
		"lora", "adapter", "prefix tuning",
		"rag", "dense retrieval", "sparse retrieval",
		"benchmark", "evaluation", "metric",
		"optimization", "scheduler", "curriculum",
		"data augmentation", "synthetic data",
		"structured output", "constrained decoding",
		"prefix caching", "kv cache", "memory",
		"token pruning", "early exit",
	}

	for _, tw := range techniqueWords {
		if strings.Contains(text, tw) {
			keywords[tw] = true
		}
	}

	return keywords
}

// scoreCluster scores a problem space cluster by commercial potential.
func scoreCluster(spaceName string, papers []paperWithKeywords) float64 {
	score := 0.0

	// Diversity bonus: count unique keywords across all 7 papers
	allKeywords := make(map[string]bool)
	for _, pw := range papers {
		for kw := range pw.keywords {
			allKeywords[kw] = true
		}
	}
	// More diverse techniques = higher score
	score += math.Min(40, float64(len(allKeywords))*2)

	// Recency bonus: average recency of papers
	recentCount := 0
	for _, pw := range papers {
		pub, err := time.Parse(time.RFC3339, pw.paper.Published)
		if err != nil {
			pub, _ = time.Parse("2006-01-02", pw.paper.Published)
		}
		if time.Since(pub).Hours()/24 <= 7 {
			recentCount++
		}
	}
	score += float64(recentCount) * 5

	// Category relevance bonus
	catBonus := map[string]float64{
		"cs.SE": 3, "cs.CL": 2.5, "cs.AI": 2, "cs.LG": 1.5, "cs.CV": 1,
	}
	for _, pw := range papers {
		if bonus, ok := catBonus[pw.category]; ok {
			score += bonus
		}
	}

	return math.Min(100, score)
}

func hasRelevantCategory(cats []string) bool {
	relevant := map[string]bool{
		"cs.AI": true, "cs.CL": true, "cs.LG": true,
		"cs.SE": true, "cs.CV": true, "stat.ML": true,
	}
	for _, c := range cats {
		if relevant[c] {
			return true
		}
	}
	return false
}
