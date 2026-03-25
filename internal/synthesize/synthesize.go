// Package synthesize generates FUSION product specs from research contexts via Claude.
// Each spec combines techniques from 7 different papers into one unified product.
package synthesize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// Synthesizer generates fusion product specs from research contexts.
type Synthesizer struct {
	cfg    *config.Config
	db     *db.DB
	client *http.Client
}

// New creates a Synthesizer.
func New(cfg *config.Config, database *db.DB) *Synthesizer {
	return &Synthesizer{
		cfg: cfg,
		db:  database,
		client: &http.Client{
			Timeout: 120 * time.Second, // LLM calls can be slow
		},
	}
}

// SynthesizeSpec generates a fusion product spec from a research context.
func (s *Synthesizer) SynthesizeSpec(ctx *types.FusionResearchContext) (*types.ProductSpec, error) {
	log.Printf("[synthesize] generating fusion spec for cluster '%s' (%d techniques)",
		ctx.Cluster.ProblemSpace, len(ctx.Techniques))

	prompt := buildPrompt(ctx)

	spec, err := s.callLLM(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed for cluster '%s': %w", ctx.Cluster.ProblemSpace, err)
	}

	// Populate source fields
	spec.ProblemSpace = ctx.Cluster.ProblemSpace
	spec.SourcePapers = ctx.Cluster.PaperIDs
	for _, r := range ctx.Repos {
		spec.SourceRepos = append(spec.SourceRepos, r.URL)
	}

	// Quality gate: check if the product already exists
	alreadyShipped, err := s.db.IsIdeaShipped(spec.Name)
	if err != nil {
		log.Printf("[synthesize] warning: shipped check failed for %s: %v", spec.Name, err)
	}
	if alreadyShipped {
		if err := s.db.MarkClusterSkipped(ctx.Cluster.ID); err != nil {
			log.Printf("[synthesize] warning: failed to mark cluster %d as skipped: %v", ctx.Cluster.ID, err)
		}
		return nil, fmt.Errorf("product %s already shipped", spec.Name)
	}

	// Save to database
	if err := s.db.SaveClusterSpec(ctx.Cluster.ID, spec); err != nil {
		return nil, fmt.Errorf("saving spec: %w", err)
	}

	log.Printf("[synthesize] generated fusion spec: %s (language: %s, ~%d lines, %d techniques fused)",
		spec.Name, spec.Language, spec.EstimatedLines, len(spec.TechniqueMap))

	return spec, nil
}

// SynthesizeAll generates specs for a batch of research contexts.
func (s *Synthesizer) SynthesizeAll(contexts []*types.FusionResearchContext) []*types.ProductSpec {
	limit := s.cfg.SpecsPerRun
	var specs []*types.ProductSpec

	for i, ctx := range contexts {
		if len(specs) >= limit {
			log.Printf("[synthesize] reached spec limit (%d), stopping", limit)
			break
		}

		log.Printf("[synthesize] processing %d/%d: '%s'", i+1, len(contexts), ctx.Cluster.ProblemSpace)

		spec, err := s.SynthesizeSpec(ctx)
		if err != nil {
			log.Printf("[synthesize] skipping cluster '%s': %v", ctx.Cluster.ProblemSpace, err)
			continue
		}
		specs = append(specs, spec)
	}

	log.Printf("[synthesize] generated %d fusion specs from %d research contexts", len(specs), len(contexts))
	return specs
}

func (s *Synthesizer) callLLM(prompt string) (*types.ProductSpec, error) {
	// Use Claude Code CLI for synthesis — gets Opus 4.6 via Max subscription.
	claudeBinary := "claude"

	cmd := exec.CommandContext(context.Background(), claudeBinary,
		"-p", prompt,
		"--output-format", "text",
		"--max-turns", "3",
	)
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude CLI: %w (stderr: %s)", err, stderr.String()[:min(200, stderr.Len())])
	}

	content := stdout.String()
	if content == "" {
		content = stderr.String()
	}

	// Extract JSON from the response (LLM might wrap it in markdown code blocks)
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in LLM response")
	}

	var spec types.ProductSpec
	if err := json.Unmarshal([]byte(jsonStr), &spec); err != nil {
		return nil, fmt.Errorf("parsing product spec JSON: %w", err)
	}

	// Validate essential fields
	if spec.Name == "" || spec.Problem == "" || spec.Solution == "" {
		return nil, fmt.Errorf("spec missing required fields (name, problem, or solution)")
	}

	return &spec, nil
}

// buildPrompt constructs the fusion synthesis prompt for the LLM.
func buildPrompt(ctx *types.FusionResearchContext) string {
	var b strings.Builder

	b.WriteString(`You are a senior product engineer designing FUSION products. A fusion product combines techniques from 7 DIFFERENT research papers into ONE unified tool that is more powerful than any individual technique alone.

You have shipped 20+ developer tools that each generate $10K-$500K ARR. You think in terms of buyers, price points, and distribution — not just cool technology.

This product must be DEPLOYABLE into our factory pipeline. It should improve one of: code generation quality, test coverage, idea discovery, paper research, prompt optimization, build throughput, or product quality. Design it as a Go binary that plugs into our existing K8s infrastructure.

`)

	// ── Problem Space ──────────────────────────────────────────────────
	b.WriteString(fmt.Sprintf("## Problem Space: %s\n\n", ctx.Cluster.ProblemSpace))

	// ── The 7 Techniques To Fuse ──────────────────────────────────────
	b.WriteString("## The 7 Techniques To Fuse\n\n")
	b.WriteString("Each paper contributes a DIFFERENT technique. Your product must use ALL 7.\n\n")

	for i, tech := range ctx.Techniques {
		b.WriteString(fmt.Sprintf("### Paper %d: %s (arXiv:%s)\n", i+1, tech.Title, tech.ArxivID))
		b.WriteString(fmt.Sprintf("**Technique:** %s\n", tech.KeyTechnique))
		if tech.Abstract != "" {
			abstract := tech.Abstract
			if len(abstract) > 1500 {
				abstract = abstract[:1500] + "..."
			}
			b.WriteString(fmt.Sprintf("**Abstract:** %s\n", abstract))
		}
		b.WriteString("\n")
	}

	// ── Related repos (competitive landscape) ────────────────────────
	if len(ctx.Repos) > 0 {
		b.WriteString("## Existing Implementations (competitive landscape)\n\n")
		b.WriteString("These repos implement INDIVIDUAL techniques. Your fusion product must beat ALL of them by combining techniques.\n\n")
		for i, r := range ctx.Repos {
			b.WriteString(fmt.Sprintf("### Repo %d: %s\n", i+1, r.Name))
			b.WriteString(fmt.Sprintf("- **URL:** %s\n", r.URL))
			b.WriteString(fmt.Sprintf("- **Stars:** %d\n", r.Stars))
			b.WriteString(fmt.Sprintf("- **Language:** %s\n", r.Language))
			b.WriteString(fmt.Sprintf("- **Description:** %s\n", r.Description))
			if r.README != "" {
				readme := r.README
				if len(readme) > 1000 {
					readme = readme[:1000] + "..."
				}
				b.WriteString(fmt.Sprintf("- **README excerpt:** %s\n", readme))
			}
			b.WriteString("\n")
		}
	}

	// ── The fusion task ──────────────────────────────────────────────
	b.WriteString(`## Your Task: Design a FUSION Product

Design a product that COMBINES all 7 techniques into something none of them could do alone.

### Fusion Rules
1. The product must use techniques from ALL 7 papers, not just 1 or 2
2. Each paper's technique must contribute a specific, named capability
3. The product must be MORE than the sum of its parts — the combination must enable something new
4. Name it something that conveys the fusion/combination aspect
5. It must be a Go binary deployable to Kubernetes
6. It must improve our factory pipeline (code generation, testing, research, or product quality)

### Step 1: Technique Integration Map

For EACH of the 7 papers, write one sentence: "Paper X's technique of [specific technique] enables the product to [specific capability]."

All 7 must compose into a coherent system, not just 7 features bolted together.

### Step 2: The Fusion Advantage

What can this combined system do that NO individual technique could do alone? This is the core value proposition. Be specific.

### Step 3: Factory Integration

Which component of our software factory does this improve?
- idea-engine (paper discovery, research, synthesis)
- claude-code-factory (code generation, building, testing)
- arxiv-archive (paper fetching, embedding, similarity)
- llm-router (inference routing, caching, optimization)
- Infrastructure (deployment, monitoring, scaling)

Describe exactly how this product plugs in: what API it exposes, what service it replaces or augments, what the deployment looks like.

### Step 4: Buyer and Market

Be brutally specific:
- **Job title**: "Senior Backend Engineer at a Series B startup" not "developers"
- **Company size**: "50-500 employees"
- **Current pain**: What they do today and why it fails
- **Price point**: Specific dollar amounts
- **Distribution**: How they install it (go install, docker pull, brew, etc.)

### Step 5: Technical Design

- **Form factor**: CLI tool? HTTP API? Library? Daemon?
- **Core API surface**: 3-7 key commands or endpoints with real signatures
- **Data model**: 2-5 core types with actual fields
- **Key dependency**: The ONE most important library

### Step 6: V1 Scope

Define exactly what ships in v1: one coherent fusion, done well. All 7 techniques must be present but the scope must be achievable in one build session.

---

Now output a single JSON object with this EXACT schema:

` + "```json\n" + `{
  "name": "kebab-case-fusion-product-name",
  "problem": "2-3 sentences: what this COMBINATION solves that individual techniques cannot.",
  "solution": "3-4 sentences: how all 7 techniques work together as one system.",
  "language": "Go",
  "files": ["main.go", "internal/engine/engine.go", "...every real file..."],
  "estimated_lines": 4000,
  "technique_map": {
    "arxiv_id_1": "what technique from this paper is used and how",
    "arxiv_id_2": "what technique from this paper is used and how",
    "arxiv_id_3": "...",
    "arxiv_id_4": "...",
    "arxiv_id_5": "...",
    "arxiv_id_6": "...",
    "arxiv_id_7": "..."
  },
  "source_papers": [],
  "source_repos": [],
  "problem_space": "",
  "market_analysis": "3-5 sentences: buyer, price, revenue model, distribution, TAM.",
  "buyer_persona": "Exact buyer: job title, company size, industry.",
  "price_point": "$X/mo individual, $X/seat/mo team, $X/mo enterprise",
  "competing_tools": ["tool-1 ($X/mo)", "tool-2 (open-source)", "manual-process (Y hrs/week)"],
  "core_algorithm": "The FUSION algorithm: how the 7 techniques compose into one system.",
  "api_design": "3-7 key commands/endpoints with actual signatures.",
  "differentiation": "Why the fusion beats every individual tool. Name each competing tool.",
  "moat": "The combinatorial advantage — why combining 7 techniques is hard to replicate.",
  "v1_scope": "What's in v1, what's out, the 30-second demo.",
  "factory_integration": "Which pipeline component this improves and how.",
  "deployment_target": "Which K8s service this replaces or augments."
}
` + "```\n\n" + `HARD REQUIREMENTS — your spec will be REJECTED if any of these fail:

1. **name**: kebab-case, memorable, conveys fusion. 2-3 words max.
2. **language**: Go. This deploys to our K8s cluster on ARM64.
3. **technique_map**: Must have exactly 7 entries, one per paper. Each must describe a SPECIFIC technique and how it's used.
4. **files**: Every real file. Must include tests. 15-40 files. No "..." entries.
5. **estimated_lines**: 3000-5000. Focused v1, not a platform.
6. **buyer_persona**: Job title + company size + industry. "Developers" is rejected.
7. **price_point**: Dollar amounts required. "Free" is rejected.
8. **competing_tools**: 2-5 real named tools or manual processes with costs.
9. **core_algorithm**: Must describe how the 7 techniques COMPOSE, not just list them.
10. **factory_integration**: Must name a specific pipeline component (idea-engine, claude-code-factory, arxiv-archive, llm-router, or infrastructure).
11. **deployment_target**: Must name a specific K8s service or say "new service: <name>".
12. **differentiation**: Must explain why the COMBINATION beats individual tools.

Output ONLY the JSON object. No markdown wrapping, no explanation — just the raw JSON.`)

	return b.String()
}

// extractJSON finds the first JSON object in a string, handling markdown code blocks.
func extractJSON(s string) string {
	// Try to extract from markdown code block first
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + 3
		// Skip optional language identifier on same line
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start = start + nl + 1
		}
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Try to find raw JSON object
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}

	// Find matching closing brace
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	return ""
}
