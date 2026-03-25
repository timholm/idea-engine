# idea-engine

Autonomous research agent that fuses 7 diverse arXiv papers into one FUSION product spec.

## Architecture

Single Go binary with cobra CLI. Pipeline stages: discover clusters -> research techniques -> synthesize fusion -> deliver.

The key insight: instead of building a product from 1 paper + 6 similar papers, we find 7 papers that each contribute a DIFFERENT technique to the same problem space, then fuse all 7 into one product that's more powerful than any alone.

```
main.go                           CLI entry point (cobra commands)
internal/config/config.go         Environment variable loading
internal/db/db.go                 PostgreSQL connection + schema (clusters, shipped_ideas)
internal/discover/discover.go     Step 1: Find problem spaces, cluster 7 diverse papers per space
internal/research/papers.go       Step 2a: Extract key technique from each of the 7 papers
internal/research/repos.go        Step 2b: Find 7 GitHub repos for the problem space
internal/research/research.go     Orchestrates technique extraction + repo research
internal/synthesize/synthesize.go Step 3: Send 7 techniques to Claude, get FUSION ProductSpec
internal/deliver/deliver.go       Step 4: Write fusion specs to factory specs directory
internal/types/types.go           Shared types (PaperCluster, FusionResearchContext, ProductSpec)
internal/api/server.go            HTTP monitoring API (/status, /clusters, /specs, /stats)
```

## Fusion Pipeline

1. **Discover**: Fetch recent papers, group by problem space keywords, pick 7 most DIVERSE papers per space (greedy max-diversity selection by technique keywords)
2. **Research**: Extract the key technique from each paper's abstract (no "find similar" step). Find repos related to the problem space.
3. **Synthesize**: Send all 7 techniques + repos to Claude. Prompt requires technique_map with all 7 papers, factory_integration, and deployment_target. Products must improve the factory pipeline itself.
4. **Deliver**: Write fusion spec JSON to factory specs directory for claude-code-factory to build.

## Dependencies

- **arxiv-archive** (HTTP API at ARCHIVE_URL): paper metadata, full text, search
- **Claude Code CLI** (`claude -p`): synthesis via Opus 4.6 (Max subscription)
- **GitHub API** (GITHUB_TOKEN): repo search, README fetch, file tree
- **PostgreSQL** (POSTGRES_URL): cluster tracking, spec storage, deduplication

## Commands

```
idea-engine run                     Full fusion pipeline
idea-engine discover                Find paper clusters (7 diverse papers per problem space)
idea-engine research <cluster_id>   Extract techniques from cluster's 7 papers + find repos
idea-engine synthesize <cluster_id> Generate fusion product spec for one cluster
idea-engine deliver                 Push ready fusion specs to factory
idea-engine list [-s status]        Show pipeline status
idea-engine stats                   Throughput and success rate
idea-engine serve [--addr :8090]    HTTP monitoring API
```

## Environment Variables

Required: ARCHIVE_URL, LLM_ROUTER_URL, GITHUB_TOKEN, POSTGRES_URL
Optional: FACTORY_SPECS_DIR, CANDIDATES_PER_RUN (default 30), SPECS_PER_RUN (default 15)

## Build & Test

```
go build ./...
go test -race ./...
docker buildx build --platform linux/arm64 -t ghcr.io/timholm/idea-engine:latest --push .
```

## Key Design Decisions

- Products fuse 7 DIFFERENT techniques, not 1 technique + 6 supporting papers
- Diversity selection: greedy algorithm picks papers with maximum keyword novelty
- Every product must improve the factory pipeline (self-improvement loop)
- ProductSpec includes technique_map (7 entries), factory_integration, deployment_target
- DB uses clusters table (not candidates) with problem_space, paper_ids (JSON array of 7)
- Synthesis via Claude Code CLI (Opus 4.6) not llm-router (Haiku)
