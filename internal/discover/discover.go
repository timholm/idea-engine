// Package discover queries arxiv-archive to find candidate papers with product potential.
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

// trendingTopics are high-signal search queries for finding commercially viable research.
var trendingTopics = []string{
	"code generation LLM",
	"retrieval augmented generation",
	"autonomous agents",
	"multimodal reasoning",
	"efficient inference",
	"tool use language model",
	"embedding model fine-tuning",
	"knowledge graph construction",
	"anomaly detection transformer",
	"federated learning privacy",
}

// categories to query for recent papers.
var categories = []string{"cs.AI", "cs.CL", "cs.LG", "cs.SE", "cs.CV", "stat.ML"}

// Discoverer finds candidate papers from the arxiv-archive API.
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
			Timeout: 30 * time.Second,
		},
	}
}

// Run discovers candidate papers: queries recent papers and trending topics,
// scores them, and returns the top N candidates.
func (d *Discoverer) Run() ([]types.Candidate, error) {
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

	// Step 2: Trending topic searches
	for _, topic := range trendingTopics {
		papers, err := d.searchPapers(topic)
		if err != nil {
			log.Printf("[discover] warning: failed to search '%s': %v", topic, err)
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

	// Step 3: Filter and score
	candidates := d.filterAndScore(allPapers)

	// Step 4: Take top N
	limit := d.cfg.CandidatesPerRun
	if len(candidates) < limit {
		limit = len(candidates)
	}
	candidates = candidates[:limit]

	// Step 5: Persist to database
	for i := range candidates {
		id, err := d.db.InsertCandidate(candidates[i].ArxivID, candidates[i].Title, candidates[i].Score)
		if err != nil {
			log.Printf("[discover] warning: failed to insert candidate %s: %v", candidates[i].ArxivID, err)
			continue
		}
		candidates[i].ID = id
	}

	log.Printf("[discover] selected %d candidates for research", len(candidates))
	return candidates, nil
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

	var papers []types.ArchivePaper
	if err := json.NewDecoder(resp.Body).Decode(&papers); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", endpoint, err)
	}
	return papers, nil
}

// filterAndScore applies quality filters and scores each paper.
func (d *Discoverer) filterAndScore(papers []types.ArchivePaper) []types.Candidate {
	cutoff := time.Now().AddDate(0, 0, -30)
	var candidates []types.Candidate

	for _, p := range papers {
		// Filter: must have published date within last 30 days
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

		// Filter: must have full text available
		if !p.HasFullText {
			continue
		}

		// Filter: must be in a relevant CS/AI category
		if !hasRelevantCategory(p.Categories) {
			continue
		}

		score := scoreCandidate(p, pub)

		candidates = append(candidates, types.Candidate{
			ArxivID:    p.ArxivID,
			Title:      p.Title,
			Abstract:   p.Abstract,
			Categories: p.Categories,
			Published:  pub,
			Score:      score,
			Status:     "pending",
		})
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates
}

// scoreCandidate calculates a commercial potential score (0-100) for a paper.
func scoreCandidate(p types.ArchivePaper, published time.Time) float64 {
	score := 0.0

	// Citation velocity: more citations relative to age = higher score
	daysSincePublished := math.Max(1, time.Since(published).Hours()/24)
	citationVelocity := float64(p.CitedBy) / daysSincePublished
	score += math.Min(30, citationVelocity*10)

	// Recency bonus: newer papers get a boost
	if daysSincePublished <= 7 {
		score += 20
	} else if daysSincePublished <= 14 {
		score += 10
	} else if daysSincePublished <= 21 {
		score += 5
	}

	// Category relevance: cs.SE and cs.CL papers tend to produce better products
	for _, cat := range p.Categories {
		switch cat {
		case "cs.SE":
			score += 15 // software engineering = directly buildable
		case "cs.CL":
			score += 12 // NLP/LLM = high commercial demand
		case "cs.AI":
			score += 10
		case "cs.LG":
			score += 8
		case "cs.CV":
			score += 5
		}
	}

	// Novelty signal: title keywords that suggest new techniques
	titleLower := strings.ToLower(p.Title)
	noveltyKeywords := []string{
		"novel", "new approach", "state-of-the-art", "outperforms",
		"efficient", "lightweight", "scalable", "real-time",
		"framework", "system", "tool", "benchmark",
	}
	for _, kw := range noveltyKeywords {
		if strings.Contains(titleLower, kw) {
			score += 3
			break
		}
	}

	// Product signal: title suggests something buildable
	productKeywords := []string{
		"agent", "code", "programming", "api", "search",
		"retrieval", "generation", "detection", "monitoring",
		"optimization", "pipeline", "automated", "autonomous",
	}
	for _, kw := range productKeywords {
		if strings.Contains(titleLower, kw) {
			score += 5
			break
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
