# idea-engine

Autonomous research agent that turns arXiv papers into monetizable product specs.

## Architecture

Single Go binary with cobra CLI. Pipeline stages: discover -> research -> synthesize -> deliver.

```
main.go                           CLI entry point (cobra commands)
internal/config/config.go         Environment variable loading
internal/db/db.go                 PostgreSQL connection + schema (candidates, shipped_ideas)
internal/discover/discover.go     Step 1: Query arxiv-archive for candidate papers
internal/research/papers.go       Step 2a: Find 7 related papers per candidate
internal/research/repos.go        Step 2b: Find 7 GitHub repos per candidate
internal/research/research.go     Orchestrates paper + repo research
internal/synthesize/synthesize.go Step 3: Send context to Claude via llm-router, get ProductSpec
internal/deliver/deliver.go       Step 4: Write specs to factory specs directory
internal/types/types.go           Shared types (Candidate, ProductSpec, PaperSummary, RepoSummary)
internal/api/server.go            HTTP monitoring API (/status, /candidates, /specs, /stats)
```

## Dependencies

- **arxiv-archive** (HTTP API at ARCHIVE_URL): paper metadata, full text, vector similarity, citations
- **llm-router** (HTTP API at LLM_ROUTER_URL): all Claude calls for synthesis
- **GitHub API** (GITHUB_TOKEN): repo search, README fetch, file tree
- **PostgreSQL** (POSTGRES_URL): candidate tracking, spec storage, deduplication

## Commands

```
idea-engine run                   Full pipeline
idea-engine discover              Find candidate papers only
idea-engine research <arxiv_id>   Deep research one paper
idea-engine synthesize <arxiv_id> Generate product spec for one paper
idea-engine deliver               Push ready specs to factory
idea-engine list [-s status]      Show pipeline status
idea-engine stats                 Throughput and success rate
idea-engine serve [--addr :8090]  HTTP monitoring API
```

## Environment Variables

Required: ARCHIVE_URL, LLM_ROUTER_URL, GITHUB_TOKEN, POSTGRES_URL
Optional: FACTORY_SPECS_DIR, CANDIDATES_PER_RUN (default 30), SPECS_PER_RUN (default 15)

## Build & Test

```
make build    # produces ./idea-engine binary
make test     # runs all tests with race detector
make lint     # go vet
```

## Data Flow

arxiv-archive provides papers -> idea-engine researches and synthesizes -> specs written to FACTORY_SPECS_DIR -> claude-code-factory builds products from specs.

## Key Design Decisions

- All LLM calls go through llm-router (never direct Anthropic API)
- Research context is cached in PostgreSQL to allow re-synthesis without re-research
- Deduplication via shipped_ideas table prevents building the same product twice
- Scoring uses citation velocity, category relevance, recency, and keyword signals
- GitHub repo filtering requires stars > 10 and activity within 12 months
