package types

import (
	"encoding/json"
	"testing"
)

func TestProductSpec_JSON(t *testing.T) {
	spec := ProductSpec{
		Name:           "inference-forge",
		Problem:        "Individual inference optimizations work in isolation but don't compose",
		Solution:       "Fuses 7 techniques: speculative decoding + KV cache compression + continuous batching + quantization + token pruning + prefix caching + structured output",
		Language:       "Go",
		Files:          []string{"main.go", "internal/engine/engine.go"},
		EstimatedLines: 4000,
		TechniqueMap: map[string]string{
			"2603.00001": "Speculative decoding for draft-verify pattern",
			"2603.00002": "KV cache compression to reduce memory",
		},
		SourcePapers:   []string{"2603.00001", "2603.00002", "2603.00003", "2603.00004", "2603.00005", "2603.00006", "2603.00007"},
		SourceRepos:    []string{"https://github.com/example/repo"},
		ProblemSpace:   "LLM inference optimization",
		MarketAnalysis: "Platform engineers at Series B-D startups pay $99/mo for unified inference optimization",
		FactoryIntegration: "Improves llm-router inference throughput by 3x",
		DeploymentTarget:   "Augments llm-router deployment in factory namespace",
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProductSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Name != spec.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, spec.Name)
	}
	if decoded.EstimatedLines != spec.EstimatedLines {
		t.Errorf("EstimatedLines = %d, want %d", decoded.EstimatedLines, spec.EstimatedLines)
	}
	if len(decoded.SourcePapers) != 7 {
		t.Errorf("SourcePapers length = %d, want 7", len(decoded.SourcePapers))
	}
	if len(decoded.TechniqueMap) != 2 {
		t.Errorf("TechniqueMap length = %d, want 2", len(decoded.TechniqueMap))
	}
	if decoded.ProblemSpace != "LLM inference optimization" {
		t.Errorf("ProblemSpace = %q, want %q", decoded.ProblemSpace, "LLM inference optimization")
	}
	if decoded.FactoryIntegration == "" {
		t.Error("FactoryIntegration should not be empty")
	}
	if decoded.DeploymentTarget == "" {
		t.Error("DeploymentTarget should not be empty")
	}
}

func TestPaperCluster_JSON(t *testing.T) {
	cluster := PaperCluster{
		ID:           1,
		ProblemSpace: "LLM inference optimization",
		PaperIDs:     []string{"2603.00001", "2603.00002", "2603.00003", "2603.00004", "2603.00005", "2603.00006", "2603.00007"},
		Score:        85.5,
		Status:       "pending",
	}

	data, err := json.Marshal(cluster)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded PaperCluster
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ProblemSpace != "LLM inference optimization" {
		t.Errorf("ProblemSpace = %q, want %q", decoded.ProblemSpace, "LLM inference optimization")
	}
	if len(decoded.PaperIDs) != 7 {
		t.Errorf("PaperIDs length = %d, want 7", len(decoded.PaperIDs))
	}
	if decoded.Score != 85.5 {
		t.Errorf("Score = %f, want 85.5", decoded.Score)
	}
}

func TestFusionResearchContext_JSON(t *testing.T) {
	ctx := FusionResearchContext{
		Cluster: PaperCluster{
			ProblemSpace: "code generation",
			PaperIDs:     []string{"2603.00001"},
		},
		Techniques: []TechniqueSummary{
			{ArxivID: "2603.00001", Title: "Paper 1", KeyTechnique: "Novel technique X"},
		},
		Repos: []RepoSummary{
			{URL: "https://github.com/test/repo", Name: "test/repo", Stars: 100, Relevance: 0.8},
		},
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded FusionResearchContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Cluster.ProblemSpace != "code generation" {
		t.Errorf("Cluster.ProblemSpace = %q, want %q", decoded.Cluster.ProblemSpace, "code generation")
	}
	if len(decoded.Techniques) != 1 {
		t.Errorf("Techniques length = %d, want 1", len(decoded.Techniques))
	}
	if decoded.Techniques[0].KeyTechnique != "Novel technique X" {
		t.Errorf("KeyTechnique = %q, want %q", decoded.Techniques[0].KeyTechnique, "Novel technique X")
	}
}

func TestStats_JSON(t *testing.T) {
	stats := Stats{
		TotalClusters: 100,
		Pending:       30,
		Delivered:     50,
		AvgScore:      72.5,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Stats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalClusters != 100 {
		t.Errorf("TotalClusters = %d, want 100", decoded.TotalClusters)
	}
	if decoded.AvgScore != 72.5 {
		t.Errorf("AvgScore = %f, want 72.5", decoded.AvgScore)
	}
}
