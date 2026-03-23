# idea-engine

Autonomous research agent that turns arXiv papers into monetizable product specs. Discovers promising research, gathers deep context (7 related papers + 7 GitHub repos per concept), and synthesizes product specifications via Claude through llm-router.

Part of a three-repo pipeline: **arxiv-archive** (data) -> **idea-engine** (intelligence) -> **claude-code-factory** (execution).

## How it works

```
idea-engine run

Step 1: Discover
  Query arxiv-archive for recent CS/AI papers
  Score by citation velocity, category relevance, novelty
  Select top 30 candidates

Step 2: Research (per candidate)
  Find 7 related papers (vector similarity + citations + references)
  Find 7 GitHub repos (keyword search + paper-cited URLs)
  Fetch READMEs and file trees for each repo

Step 3: Synthesize (per candidate)
  Send candidate + 7 papers + 7 repos to Claude via llm-router
  Receive structured ProductSpec JSON
  Quality gate: reject duplicates, low-novelty specs

Step 4: Deliver
  Write specs to factory specs directory
  Track in PostgreSQL to prevent duplicates
  Output: 10-15 production-ready product specs per run
```

## Install

```bash
git clone https://github.com/timholm/idea-engine
cd idea-engine
make build
```

## Usage

```bash
# Full pipeline (designed for daily cron at 5 AM, after arxiv-archive sync)
idea-engine run

# Individual stages
idea-engine discover              # find candidate papers
idea-engine research 2603.16514   # deep research one paper
idea-engine synthesize 2603.16514 # generate product spec
idea-engine deliver               # push specs to factory

# Monitoring
idea-engine list                  # pipeline status
idea-engine list -s synthesized   # filter by status
idea-engine stats                 # throughput and success rate
idea-engine serve --addr :8090    # HTTP monitoring API
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ARCHIVE_URL` | yes | | arxiv-archive HTTP API base URL |
| `LLM_ROUTER_URL` | yes | | llm-router base URL for Claude calls |
| `GITHUB_TOKEN` | yes | | GitHub personal access token |
| `POSTGRES_URL` | yes | | PostgreSQL connection string |
| `FACTORY_SPECS_DIR` | no | `specs/` | Directory to write specs for factory |
| `CANDIDATES_PER_RUN` | no | `30` | Max candidates to discover per run |
| `SPECS_PER_RUN` | no | `15` | Max specs to output per run |

## Output format

Each spec is a JSON file written to `FACTORY_SPECS_DIR`:

```json
{
  "name": "semantic-code-nav",
  "problem": "Developers waste 30% of time navigating unfamiliar codebases",
  "solution": "Embedding-based code search combining techniques from papers on code understanding with the UX patterns of existing tools, exposed as a CLI that integrates with editors",
  "language": "Go",
  "files": ["main.go", "internal/index/index.go", "internal/search/search.go", "internal/embed/embed.go"],
  "estimated_lines": 4500,
  "source_papers": ["2603.16514", "2603.12001", "2603.09877", "2602.18234", "2602.15001", "2601.22345", "2601.19876"],
  "source_repos": ["https://github.com/sourcegraph/scip", "https://github.com/BloopAI/bloop", "..."],
  "source_url": "https://arxiv.org/abs/2603.16514",
  "market_analysis": "Enterprise dev teams ($50/seat/month). Moat: combines latest embedding research with production-grade Go implementation. Competing tools are Python-only or lack semantic search."
}
```

## Monitoring API

When running `idea-engine serve`:

```
GET /status         Pipeline health and aggregate stats
GET /candidates     List candidates (optional ?status= filter)
GET /candidates/:id Full candidate detail with research context and spec
GET /specs          All generated product specs
GET /stats          Pipeline statistics
```

## Database

Uses PostgreSQL (can share instance with arxiv-archive). Schema auto-created on startup:

- `candidates` — tracks papers through the pipeline (pending -> researching -> synthesized -> delivered)
- `shipped_ideas` — deduplication table, prevents building the same product twice

## Architecture

```
                    arxiv-archive (:9090)
                         |
                    HTTP API (papers, similarity, citations)
                         |
                    idea-engine
                    |    |    |
          discover  | research | synthesize
          (archive) | (archive | (llm-router)
                    | +github) |
                         |
                    deliver (write JSON to factory specs dir)
                         |
                    claude-code-factory (reads specs, builds products)
```

## License

MIT
