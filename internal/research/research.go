package research

import (
	"fmt"
	"log"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// Researcher orchestrates paper + repo research for candidates.
type Researcher struct {
	cfg    *config.Config
	db     *db.DB
	papers *PaperResearcher
	repos  *RepoResearcher
}

// New creates a Researcher.
func New(cfg *config.Config, database *db.DB) *Researcher {
	return &Researcher{
		cfg:    cfg,
		db:     database,
		papers: NewPaperResearcher(cfg),
		repos:  NewRepoResearcher(cfg),
	}
}

// ResearchCandidate performs deep research on a single candidate paper.
// Returns the full research context (candidate + 7 papers + 7 repos).
func (r *Researcher) ResearchCandidate(arxivID string) (*types.ResearchContext, error) {
	log.Printf("[research] starting deep research for %s", arxivID)

	// Update status to researching
	if err := r.db.UpdateStatus(arxivID, "researching"); err != nil {
		return nil, fmt.Errorf("updating status: %w", err)
	}

	// Fetch the full paper from archive
	paper, err := r.papers.FetchFullPaper(arxivID)
	if err != nil {
		return nil, fmt.Errorf("fetching paper %s: %w", arxivID, err)
	}

	candidate := types.Candidate{
		ArxivID:  paper.ArxivID,
		Title:    paper.Title,
		Abstract: paper.Abstract,
		FullText: paper.FullText,
	}

	// Find 7 related papers
	relatedPapers, err := r.papers.FindRelatedPapers(arxivID)
	if err != nil {
		log.Printf("[research] warning: paper research failed for %s: %v", arxivID, err)
		relatedPapers = nil // continue with whatever we have
	}

	// Find 7 related repos
	relatedRepos, err := r.repos.FindRelatedRepos(paper.Title, paper.Abstract, paper.FullText)
	if err != nil {
		log.Printf("[research] warning: repo research failed for %s: %v", arxivID, err)
		relatedRepos = nil
	}

	// Must have at least some research to be useful
	if len(relatedPapers) == 0 && len(relatedRepos) == 0 {
		if err := r.db.MarkSkipped(arxivID); err != nil {
			log.Printf("[research] warning: failed to mark %s as skipped: %v", arxivID, err)
		}
		return nil, fmt.Errorf("no related papers or repos found for %s", arxivID)
	}

	ctx := &types.ResearchContext{
		Candidate: candidate,
		Papers:    relatedPapers,
		Repos:     relatedRepos,
	}

	// Cache the research context in the database
	if err := r.db.SaveResearch(arxivID, ctx); err != nil {
		log.Printf("[research] warning: failed to cache research for %s: %v", arxivID, err)
	}

	log.Printf("[research] completed research for %s: %d papers, %d repos",
		arxivID, len(relatedPapers), len(relatedRepos))

	return ctx, nil
}

// ResearchAll performs deep research on a batch of candidates.
// Returns the successfully researched contexts.
func (r *Researcher) ResearchAll(candidates []types.Candidate) []*types.ResearchContext {
	var results []*types.ResearchContext

	for i, c := range candidates {
		log.Printf("[research] processing candidate %d/%d: %s", i+1, len(candidates), c.ArxivID)

		ctx, err := r.ResearchCandidate(c.ArxivID)
		if err != nil {
			log.Printf("[research] skipping %s: %v", c.ArxivID, err)
			continue
		}
		results = append(results, ctx)
	}

	log.Printf("[research] completed %d/%d candidates successfully", len(results), len(candidates))
	return results
}
