// Package synthesize generates product specs from research contexts via Claude through llm-router.
package synthesize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	reqBody := types.LLMRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.LLMMessage{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling LLM request: %w", err)
	}

	u := fmt.Sprintf("%s/v1/chat/completions", s.cfg.LLMRouterURL)
	resp, err := s.client.Post(u, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM router returned %d: %s", resp.StatusCode, string(body))
	}

	var llmResp types.LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, fmt.Errorf("decoding LLM response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}

	content := llmResp.Choices[0].Message.Content

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

	b.WriteString("You are a product engineer. Your job is to design a novel, monetizable software product based on cutting-edge research.\n\n")

	// Primary paper
	b.WriteString("## Primary Research Paper\n\n")
	b.WriteString(fmt.Sprintf("**Title:** %s\n", ctx.Candidate.Title))
	b.WriteString(fmt.Sprintf("**ArXiv ID:** %s\n\n", ctx.Candidate.ArxivID))

	if ctx.Candidate.FullText != "" {
		// Truncate full text to ~8000 chars to leave room for context
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

	// Related papers
	if len(ctx.Papers) > 0 {
		b.WriteString("## Related Papers (7 most relevant)\n\n")
		for i, p := range ctx.Papers {
			b.WriteString(fmt.Sprintf("### Paper %d: %s\n", i+1, p.Title))
			b.WriteString(fmt.Sprintf("- **ArXiv ID:** %s\n", p.ArxivID))
			b.WriteString(fmt.Sprintf("- **Relevance:** %.2f\n", p.Relevance))
			b.WriteString(fmt.Sprintf("- **Abstract:** %s\n", p.Abstract))
			if p.KeyFindings != "" {
				b.WriteString(fmt.Sprintf("- **Key Finding:** %s\n", p.KeyFindings))
			}
			b.WriteString("\n")
		}
	}

	// Related repos
	if len(ctx.Repos) > 0 {
		b.WriteString("## Existing Implementations (7 most relevant GitHub repos)\n\n")
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
			if len(r.FileTree) > 0 {
				b.WriteString(fmt.Sprintf("- **File tree:** %s\n", strings.Join(r.FileTree, ", ")))
			}
			b.WriteString("\n")
		}
	}

	// Instructions
	b.WriteString(`## Your Task

Given this research paper, 7 related papers, and 7 existing implementations, design a product that:

1. **COMBINES** the best techniques from these papers into something new
2. **IMPROVES** on what existing repos do well
3. **FILLS GAPS** that existing repos leave open
4. **MONETIZES** — who pays for this? what form factor (CLI tool, API service, library)?
5. **IS BUILDABLE** — a single Go binary or focused codebase, not a research prototype

Think like a product engineer, not a researcher:
- Who is the customer? What's their pain?
- What's the form factor? (CLI, API, library, daemon)
- What's the moat? (speed, accuracy, integration, developer experience)
- How does this make money? (SaaS, open-core, enterprise license)

Output a single JSON object with this exact schema:

` + "```json\n" + `{
  "name": "kebab-case-product-name",
  "problem": "1-2 sentences describing the problem this solves",
  "solution": "2-3 sentences describing the product and its key innovation",
  "language": "Go",
  "files": ["main.go", "internal/core/engine.go", "..."],
  "estimated_lines": 3000,
  "source_papers": [],
  "source_repos": [],
  "source_url": "",
  "market_analysis": "2-3 sentences: who pays, why they pay, what the moat is"
}
` + "```\n\n" + `Requirements for the spec:
- Name must be kebab-case, memorable, and descriptive
- Language should be Go unless another language is clearly better for this domain
- Files should be realistic — list the actual source files you'd create
- estimated_lines should be realistic for a production implementation (usually 2000-8000)
- market_analysis must identify a specific buyer persona and explain the revenue model
- The product must be novel — not a clone of any existing repo listed above

Output ONLY the JSON object, no other text.`)

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
