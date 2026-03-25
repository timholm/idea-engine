package research

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// Researcher orchestrates technique extraction + repo research for paper clusters.
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

// ResearchCluster performs deep research on a paper cluster.
// Extracts the key technique from each of the 7 papers and finds related repos
// for the problem space. No "find similar papers" step — papers are already diverse.
func (r *Researcher) ResearchCluster(cluster types.PaperCluster) (*types.FusionResearchContext, error) {
	log.Printf("[research] starting fusion research for cluster '%s' (%d papers)",
		cluster.ProblemSpace, len(cluster.Papers))

	// Update status to researching
	if err := r.db.UpdateClusterStatus(cluster.ID, "researching"); err != nil {
		return nil, fmt.Errorf("updating status: %w", err)
	}

	// Step 1: Extract key technique from each paper
	techniques, err := r.papers.ExtractTechniques(cluster.Papers)
	if err != nil {
		log.Printf("[research] warning: technique extraction failed for cluster '%s': %v",
			cluster.ProblemSpace, err)
		techniques = nil
	}

	// Step 2: Find repos related to the problem space (not one paper)
	relatedRepos, err := r.repos.FindRelatedRepos(cluster.ProblemSpace, techniques)
	if err != nil {
		log.Printf("[research] warning: repo research failed for cluster '%s': %v",
			cluster.ProblemSpace, err)
		relatedRepos = nil
	}

	// Must have at least some research to be useful
	if len(techniques) == 0 && len(relatedRepos) == 0 {
		if err := r.db.MarkClusterSkipped(cluster.ID); err != nil {
			log.Printf("[research] warning: failed to mark cluster %d as skipped: %v", cluster.ID, err)
		}
		return nil, fmt.Errorf("no techniques or repos found for cluster '%s'", cluster.ProblemSpace)
	}

	ctx := &types.FusionResearchContext{
		Cluster:    cluster,
		Techniques: techniques,
		Repos:      relatedRepos,
	}

	// Cache the research context in the database
	data, err := json.Marshal(ctx)
	if err == nil {
		if err := r.db.SaveClusterResearch(cluster.ID, string(data)); err != nil {
			log.Printf("[research] warning: failed to cache research for cluster %d: %v", cluster.ID, err)
		}
	}

	log.Printf("[research] completed research for cluster '%s': %d techniques, %d repos",
		cluster.ProblemSpace, len(techniques), len(relatedRepos))

	return ctx, nil
}

// ResearchAll performs deep research on a batch of paper clusters.
// Returns the successfully researched contexts.
func (r *Researcher) ResearchAll(clusters []types.PaperCluster) []*types.FusionResearchContext {
	var results []*types.FusionResearchContext

	for i, c := range clusters {
		log.Printf("[research] processing cluster %d/%d: '%s'", i+1, len(clusters), c.ProblemSpace)

		ctx, err := r.ResearchCluster(c)
		if err != nil {
			log.Printf("[research] skipping cluster '%s': %v", c.ProblemSpace, err)
			continue
		}
		results = append(results, ctx)
	}

	log.Printf("[research] completed %d/%d clusters successfully", len(results), len(clusters))
	return results
}
