// Package research gathers deep context for paper clusters: technique extraction and GitHub repos.
package research

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/types"
)

// PaperResearcher extracts technique summaries from cluster papers.
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

// ExtractTechniques reads the abstract of each paper in the cluster and identifies
// the KEY TECHNIQUE from each — a one-sentence summary of what's novel.
// No "find similar papers" step: the 7 diverse papers are already chosen by discover.
func (pr *PaperResearcher) ExtractTechniques(papers []types.ArchivePaper) ([]types.TechniqueSummary, error) {
	var techniques []types.TechniqueSummary

	for _, p := range papers {
		// Fetch full paper data if we only have ID
		paper := p
		if paper.Abstract == "" {
			full, err := pr.FetchFullPaper(paper.ArxivID)
			if err != nil {
				log.Printf("[research/papers] warning: failed to fetch paper %s: %v", paper.ArxivID, err)
				// Use what we have
			} else {
				paper = *full
			}
		}

		technique := extractKeyTechnique(paper.Title, paper.Abstract)

		techniques = append(techniques, types.TechniqueSummary{
			ArxivID:      paper.ArxivID,
			Title:        paper.Title,
			Abstract:     paper.Abstract,
			KeyTechnique: technique,
		})
	}

	log.Printf("[research/papers] extracted %d technique summaries from cluster", len(techniques))
	return techniques, nil
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

// extractKeyTechnique identifies the primary novel technique from a paper's title and abstract.
// Returns a one-sentence summary of what's technically novel.
func extractKeyTechnique(title, abstract string) string {
	if abstract == "" {
		return title
	}

	sentences := strings.Split(abstract, ". ")
	if len(sentences) == 0 {
		return abstract
	}

	// Priority 1: Sentences that describe what this paper proposes/introduces
	proposeSignals := []string{
		"we propose", "we present", "we introduce", "we develop",
		"this paper proposes", "this paper presents", "this paper introduces",
		"our approach", "our method", "our framework", "our system",
		"key insight", "key idea", "main contribution",
	}
	for _, s := range sentences {
		lower := strings.ToLower(s)
		for _, sig := range proposeSignals {
			if strings.Contains(lower, sig) {
				return ensureTrailingPeriod(s)
			}
		}
	}

	// Priority 2: Sentences that describe results/technique
	resultSignals := []string{
		"achieves", "outperforms", "enables", "reduces",
		"improves", "demonstrates", "shows that",
	}
	for _, s := range sentences {
		lower := strings.ToLower(s)
		for _, sig := range resultSignals {
			if strings.Contains(lower, sig) {
				return ensureTrailingPeriod(s)
			}
		}
	}

	// Fallback: first sentence of abstract (usually describes the contribution)
	return ensureTrailingPeriod(sentences[0])
}

// ensureTrailingPeriod adds a period if the string doesn't already end with one.
func ensureTrailingPeriod(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if s[len(s)-1] == '.' {
		return s
	}
	return s + "."
}
