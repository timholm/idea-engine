// Package research gathers deep context for candidate papers: related papers and GitHub repos.
package research

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/types"
)

const maxRelatedPapers = 7

// PaperResearcher finds related papers for a candidate using arxiv-archive.
type PaperResearcher struct {
	cfg    *config.Config
	client *http.Client
}

// NewPaperResearcher creates a PaperResearcher.
func NewPaperResearcher(cfg *config.Config) *PaperResearcher {
	return &PaperResearcher{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FindRelatedPapers finds the top 7 related papers for a candidate.
// Uses vector similarity, citations, and references from arxiv-archive.
func (pr *PaperResearcher) FindRelatedPapers(arxivID string) ([]types.PaperSummary, error) {
	seen := make(map[string]bool)
	seen[arxivID] = true // exclude the candidate itself
	var scored []scoredPaper

	// Source 1: Vector similarity (strongest relevance signal)
	similar, err := pr.fetchSimilar(arxivID)
	if err != nil {
		log.Printf("[research/papers] warning: similar lookup failed for %s: %v", arxivID, err)
	} else {
		for i, p := range similar {
			if seen[p.ArxivID] {
				continue
			}
			seen[p.ArxivID] = true
			// Similarity results are pre-ranked; assign decreasing relevance
			relevance := 1.0 - (float64(i) * 0.05)
			scored = append(scored, scoredPaper{paper: p, relevance: relevance, source: "similar"})
		}
	}

	// Source 2: Papers that cite this one (building on this work)
	citedBy, err := pr.fetchCitedBy(arxivID)
	if err != nil {
		log.Printf("[research/papers] warning: cited-by lookup failed for %s: %v", arxivID, err)
	} else {
		for i, p := range citedBy {
			if seen[p.ArxivID] {
				continue
			}
			seen[p.ArxivID] = true
			relevance := 0.9 - (float64(i) * 0.05)
			scored = append(scored, scoredPaper{paper: p, relevance: relevance, source: "cited-by"})
		}
	}

	// Source 3: Papers this one references (foundational work)
	refs, err := pr.fetchRefs(arxivID)
	if err != nil {
		log.Printf("[research/papers] warning: refs lookup failed for %s: %v", arxivID, err)
	} else {
		for i, p := range refs {
			if seen[p.ArxivID] {
				continue
			}
			seen[p.ArxivID] = true
			relevance := 0.8 - (float64(i) * 0.05)
			scored = append(scored, scoredPaper{paper: p, relevance: relevance, source: "refs"})
		}
	}

	// Rank by relevance and pick top 7
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].relevance > scored[j].relevance
	})

	limit := maxRelatedPapers
	if len(scored) < limit {
		limit = len(scored)
	}

	results := make([]types.PaperSummary, 0, limit)
	for _, sp := range scored[:limit] {
		summary := types.PaperSummary{
			ArxivID:     sp.paper.ArxivID,
			Title:       sp.paper.Title,
			Abstract:    sp.paper.Abstract,
			KeyFindings: extractKeyFindings(sp.paper.Abstract),
			Relevance:   sp.relevance,
		}
		results = append(results, summary)
	}

	log.Printf("[research/papers] found %d related papers for %s (from %d total)", len(results), arxivID, len(scored))
	return results, nil
}

// FetchFullPaper retrieves a paper's complete data from arxiv-archive.
func (pr *PaperResearcher) FetchFullPaper(arxivID string) (*types.ArchivePaper, error) {
	u := fmt.Sprintf("%s/papers/%s", pr.cfg.ArchiveURL, arxivID)
	resp, err := pr.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s returned %d: %s", u, resp.StatusCode, string(body))
	}

	var paper types.ArchivePaper
	if err := json.NewDecoder(resp.Body).Decode(&paper); err != nil {
		return nil, fmt.Errorf("decoding paper %s: %w", arxivID, err)
	}
	return &paper, nil
}

type scoredPaper struct {
	paper     types.ArchivePaper
	relevance float64
	source    string
}

func (pr *PaperResearcher) fetchSimilar(arxivID string) ([]types.ArchivePaper, error) {
	u := fmt.Sprintf("%s/papers/similar/%s", pr.cfg.ArchiveURL, arxivID)
	return pr.fetchPaperList(u)
}

func (pr *PaperResearcher) fetchCitedBy(arxivID string) ([]types.ArchivePaper, error) {
	u := fmt.Sprintf("%s/papers/%s/cited-by", pr.cfg.ArchiveURL, arxivID)
	return pr.fetchPaperList(u)
}

func (pr *PaperResearcher) fetchRefs(arxivID string) ([]types.ArchivePaper, error) {
	u := fmt.Sprintf("%s/papers/%s/refs", pr.cfg.ArchiveURL, arxivID)
	return pr.fetchPaperList(u)
}

func (pr *PaperResearcher) fetchPaperList(endpoint string) ([]types.ArchivePaper, error) {
	resp, err := pr.client.Get(endpoint)
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
		return nil, fmt.Errorf("decoding papers from %s: %w", endpoint, err)
	}
	return papers, nil
}

// extractKeyFindings pulls the most salient sentence from an abstract.
// This is a simple heuristic; the LLM synthesis step does the real extraction.
func extractKeyFindings(abstract string) string {
	if abstract == "" {
		return ""
	}

	sentences := strings.Split(abstract, ". ")
	if len(sentences) == 0 {
		return abstract
	}

	// Look for sentences with result/finding signals
	signals := []string{
		"we propose", "we present", "we introduce", "our approach",
		"achieves", "outperforms", "state-of-the-art", "significant",
		"demonstrate", "results show", "experiments show",
	}

	for _, s := range sentences {
		lower := strings.ToLower(s)
		for _, sig := range signals {
			if strings.Contains(lower, sig) {
				return strings.TrimSpace(s) + "."
			}
		}
	}

	// Fallback: last sentence often contains results
	return strings.TrimSpace(sentences[len(sentences)-1])
}
