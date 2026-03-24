// Package synthesize generates product specs from research contexts via Claude through llm-router.
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

// Synthesizer generates product specs from research contexts.
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

// SynthesizeSpec generates a product spec from a research context.
func (s *Synthesizer) SynthesizeSpec(ctx *types.ResearchContext) (*types.ProductSpec, error) {
	log.Printf("[synthesize] generating spec for %s: %s", ctx.Candidate.ArxivID, ctx.Candidate.Title)

	prompt := buildPrompt(ctx)

	spec, err := s.callLLM(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed for %s: %w", ctx.Candidate.ArxivID, err)
	}

	// Populate source fields
	spec.SourceURL = fmt.Sprintf("https://arxiv.org/abs/%s", ctx.Candidate.ArxivID)
	for _, p := range ctx.Papers {
		spec.SourcePapers = append(spec.SourcePapers, p.ArxivID)
	}
	for _, r := range ctx.Repos {
		spec.SourceRepos = append(spec.SourceRepos, r.URL)
	}

	// Quality gate: check if the product already exists
	alreadyShipped, err := s.db.IsIdeaShipped(spec.Name)
	if err != nil {
		log.Printf("[synthesize] warning: shipped check failed for %s: %v", spec.Name, err)
	}
	if alreadyShipped {
		if err := s.db.MarkSkipped(ctx.Candidate.ArxivID); err != nil {
			log.Printf("[synthesize] warning: failed to mark %s as skipped: %v", ctx.Candidate.ArxivID, err)
		}
		return nil, fmt.Errorf("product %s already shipped", spec.Name)
	}

	// Save to database
	if err := s.db.SaveSpec(ctx.Candidate.ArxivID, spec); err != nil {
		return nil, fmt.Errorf("saving spec: %w", err)
	}

	log.Printf("[synthesize] generated spec: %s (language: %s, ~%d lines)",
		spec.Name, spec.Language, spec.EstimatedLines)

	return spec, nil
}

// SynthesizeAll generates specs for a batch of research contexts.
func (s *Synthesizer) SynthesizeAll(contexts []*types.ResearchContext) []*types.ProductSpec {
	limit := s.cfg.SpecsPerRun
	var specs []*types.ProductSpec

	for i, ctx := range contexts {
		if len(specs) >= limit {
			log.Printf("[synthesize] reached spec limit (%d), stopping", limit)
			break
		}

		log.Printf("[synthesize] processing %d/%d: %s", i+1, len(contexts), ctx.Candidate.ArxivID)

		spec, err := s.SynthesizeSpec(ctx)
		if err != nil {
			log.Printf("[synthesize] skipping %s: %v", ctx.Candidate.ArxivID, err)
			continue
		}
		specs = append(specs, spec)
	}

	log.Printf("[synthesize] generated %d specs from %d research contexts", len(specs), len(contexts))
	return specs
}

func (s *Synthesizer) callLLM(prompt string) (*types.ProductSpec, error) {
	// Use Claude Code CLI for synthesis — gets Opus 4.6 via Max subscription.
	// This is the highest-quality model for the most important step.
	// Haiku (via router) is used for reading papers; Opus designs the products.
	claudeBinary := "claude"
	if s.cfg.LLMRouterURL != "" {
		// Check if a custom claude binary is configured
		// For now just use "claude" which picks up Max subscription auth
	}

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

// buildPrompt constructs the rich context prompt for the LLM.
func buildPrompt(ctx *types.ResearchContext) string {
	var b strings.Builder

	// System identity: commercially-minded product engineer, not a researcher
	b.WriteString(`You are a senior product engineer at a dev tools company that ships profitable open-source products. You have shipped 20+ developer tools that each generate $10K-$500K ARR. You think in terms of buyers, price points, distribution channels, and switching costs — not just cool technology.

Your job: turn cutting-edge research into a SPECIFIC, BUILDABLE, SELLABLE product spec.

`)

	// ── Primary paper ───────────────────────────────────────────────────
	b.WriteString("## Primary Research Paper\n\n")
	b.WriteString(fmt.Sprintf("**Title:** %s\n", ctx.Candidate.Title))
	b.WriteString(fmt.Sprintf("**ArXiv ID:** %s\n", ctx.Candidate.ArxivID))
	b.WriteString(fmt.Sprintf("**Link:** https://arxiv.org/abs/%s\n\n", ctx.Candidate.ArxivID))

	if ctx.Candidate.FullText != "" {
		text := ctx.Candidate.FullText
		if len(text) > 8000 {
			text = text[:8000] + "\n\n[... truncated for length ...]"
		}
		b.WriteString("**Full Text (excerpt):**\n")
		b.WriteString(text)
		b.WriteString("\n\n")
	} else if ctx.Candidate.Abstract != "" {
		b.WriteString("**Abstract:**\n")
		b.WriteString(ctx.Candidate.Abstract)
		b.WriteString("\n\n")
	}

	// ── Related papers ──────────────────────────────────────────────────
	if len(ctx.Papers) > 0 {
		b.WriteString("## Related Papers (prior art and adjacent techniques)\n\n")
		for i, p := range ctx.Papers {
			b.WriteString(fmt.Sprintf("### Paper %d: %s\n", i+1, p.Title))
			b.WriteString(fmt.Sprintf("- **ArXiv ID:** %s\n", p.ArxivID))
			b.WriteString(fmt.Sprintf("- **Relevance score:** %.2f\n", p.Relevance))
			b.WriteString(fmt.Sprintf("- **Abstract:** %s\n", p.Abstract))
			if p.KeyFindings != "" {
				b.WriteString(fmt.Sprintf("- **Key technique/finding:** %s\n", p.KeyFindings))
			}
			b.WriteString("\n")
		}
	}

	// ── Related repos (competitive landscape) ───────────────────────────
	if len(ctx.Repos) > 0 {
		b.WriteString("## Existing Implementations (competitive landscape — 7 GitHub repos)\n\n")
		b.WriteString("Study these carefully. Your product must be CLEARLY DIFFERENT from every one of them.\n\n")
		for i, r := range ctx.Repos {
			b.WriteString(fmt.Sprintf("### Repo %d: %s\n", i+1, r.Name))
			b.WriteString(fmt.Sprintf("- **URL:** %s\n", r.URL))
			b.WriteString(fmt.Sprintf("- **Stars:** %d\n", r.Stars))
			b.WriteString(fmt.Sprintf("- **Language:** %s\n", r.Language))
			b.WriteString(fmt.Sprintf("- **Description:** %s\n", r.Description))
			if r.README != "" {
				readme := r.README
				if len(readme) > 1500 {
					readme = readme[:1500] + "..."
				}
				b.WriteString(fmt.Sprintf("- **README excerpt:** %s\n", readme))
			}
			if len(r.FileTree) > 0 {
				b.WriteString(fmt.Sprintf("- **File tree:** %s\n", strings.Join(r.FileTree, ", ")))
			}
			b.WriteString("\n")
		}
	}

	// ── The actual task: structured thinking followed by structured output ──
	b.WriteString(`## Your Task

Design a commercially viable developer tool based on the research above. Work through these steps IN ORDER before producing the JSON output.

### Step 1: Extract the Core Technical Insight

Read the primary paper carefully. Identify the ONE specific algorithm, technique, data structure, or method that is novel. Not the general topic — the specific technique. For example:
- NOT "a paper about code search" → YES "locality-sensitive hashing with syntax-aware tokenization for sub-100ms code search across 1M+ file repos"
- NOT "a paper about testing" → YES "mutation-guided fuzzing using abstract syntax tree rewriting to generate semantically valid test inputs"

State this technique in one sentence. This is your core_algorithm.

### Step 2: Competitive Gap Analysis

For EACH of the 7 repos above, answer:
1. What does this repo do well?
2. What does it NOT do? (missing features, poor DX, no API, slow, not maintained, wrong language)
3. Would someone pay for it? Why or why not?

Then identify the GAP: what would a tool need to do to make someone SWITCH from the best existing repo to your product? This gap is your differentiation.

### Step 3: Buyer and Market

Be brutally specific:
- **Job title**: "Senior Backend Engineer at a Series B startup" not "developers"
- **Company size**: "50-500 employees" not "companies"
- **Industry**: "fintech, healthtech, or any company with >10 microservices" not "tech"
- **Current pain**: What do they do TODAY without your tool? (manual process, expensive existing tool, fragile scripts)
- **What they pay today**: Name 1-3 competing paid tools and their price. If no paid tool exists, describe the manual cost (hours/week × engineer salary).
- **Your price point**: $X/mo for individual, $X/mo per seat for team, $X/yr for enterprise. Be specific.
- **Distribution**: How do they find and install it? (brew install, npm install, pip install, go install, docker pull, GitHub Marketplace, VS Code extension marketplace, direct sales)

### Step 4: Technical Design

Design the product as a REAL engineer would build it:
- **Form factor**: CLI tool? HTTP API server? Library? VS Code extension? GitHub Action? Daemon?
- **Core API surface**: List the 3-7 key commands (for CLI) or endpoints (for HTTP) or functions (for library). Use actual names, not placeholders.
  Example: "drift detect --dir ./infra --baseline main" not "a command to detect drift"
  Example: "POST /v1/analyze { code: string, language: string } → { findings: Finding[] }" not "an endpoint for analysis"
- **Data model**: What are the 2-5 core types/structs? Name them and list their key fields.
  Example: "Finding { file: string, line: int, severity: Level, message: string, suggestion: string }" not "a findings object"
- **Storage**: Does it need a database? What kind? (SQLite for local CLI, PostgreSQL for SaaS, none for pure library)
- **Key dependency**: What's the ONE most important library or system it depends on? (tree-sitter, LLVM, libgit2, a specific Go/Rust/Python package)

### Step 5: Realistic File Layout

List EVERY source file you would actually create. Not "..." — every file. Include:
- Entry point (main.go, src/main.rs, etc.)
- Core algorithm implementation
- CLI/HTTP layer
- Tests for core logic
- Configuration
- README, Makefile, Dockerfile

Target 3,000-5,000 lines total. This is a focused v1, not a platform.

### Step 6: V1 Scope

Define exactly what ships in v1 — one core feature, done well:
- What is IN scope (the one thing it does perfectly)
- What is OUT of scope (features for v2/v3)
- What is the "hello world" demo that proves it works in 30 seconds

---

Now output a single JSON object with this EXACT schema (no extra keys, no missing keys):

` + "```json\n" + `{
  "name": "kebab-case-product-name",
  "problem": "2-3 sentences. State the specific pain point for the specific buyer. Include what they do today and why it sucks.",
  "solution": "3-4 sentences. What the product does, what technique from the paper powers it, and why it's better than existing tools.",
  "language": "Go",
  "files": ["main.go", "internal/engine/engine.go", "internal/engine/engine_test.go", "...every real file..."],
  "estimated_lines": 4000,
  "source_papers": [],
  "source_repos": [],
  "source_url": "",
  "market_analysis": "3-5 sentences: buyer persona, price point, revenue model, distribution channel, and TAM estimate.",
  "buyer_persona": "Exact buyer: job title, company size, industry. e.g. 'Platform engineers at Series B-D startups (50-500 eng) running Kubernetes in production'",
  "price_point": "Specific pricing. e.g. '$29/mo individual, $19/seat/mo team (5+), $499/mo enterprise (unlimited seats, SSO, audit log)'",
  "competing_tools": ["tool-name-1 ($X/mo)", "tool-name-2 (open-source)", "manual-process (Y hrs/week)"],
  "core_algorithm": "One sentence: the specific technique from the paper. e.g. 'Incremental abstract interpretation with widening operators for polyhedra domains, enabling sub-second re-analysis on file save'",
  "api_design": "The 3-7 key commands or endpoints with actual signatures. e.g. 'CLI: analyze <path> [--fix] [--format json|sarif], watch <path> --on-change analyze, init [--language go|rust|python], ci --fail-on=error --baseline=main'",
  "differentiation": "2-3 sentences: exactly how this differs from each competing tool and why someone would switch.",
  "moat": "1-2 sentences: what makes this hard to replicate. Speed from a specific algorithm? Accuracy from a specific technique? Integrations? Data network effects?",
  "v1_scope": "2-3 sentences: what's in v1, what's explicitly out, and the 30-second demo."
}
` + "```\n\n" + `HARD REQUIREMENTS — your spec will be REJECTED if any of these fail:

1. **name**: kebab-case, memorable, describes what it does. 2-3 words max.
2. **language**: Go unless the domain genuinely requires Rust (performance-critical bit manipulation, WASM), Python (ML model inference, data science), or TypeScript (VS Code extension, web UI). Justify if not Go.
3. **files**: List every real file. Must include tests. No "..." entries. Minimum 15 files, maximum 40 files.
4. **estimated_lines**: Between 3000 and 5000. Not 500 (toy), not 10000 (overscoped).
5. **buyer_persona**: Must include job title, company size, AND industry. "Developers" is rejected.
6. **price_point**: Must include a dollar amount. "Free" or "open-source" is rejected — there must be a paid tier.
7. **competing_tools**: Must list 2-5 real, named tools or describe the manual process with time cost. "None" is rejected.
8. **core_algorithm**: Must reference a specific technique FROM THE PAPER. "Machine learning" or "AI-powered" is rejected.
9. **api_design**: Must include actual command names or endpoint paths with parameters. Vague descriptions are rejected.
10. **differentiation**: Must name each competing tool and explain why yours is better for the target buyer.
11. **v1_scope**: Must say what is OUT of scope, not just what's in scope.
12. **The product must NOT be a clone** of any repo listed above. It must combine the paper's technique with a novel product angle.

Output ONLY the JSON object. No markdown wrapping, no explanation before or after — just the raw JSON.`)

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
