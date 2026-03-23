# AGENTS.md — idea-engine

## What This Repo Does

idea-engine is the intelligence layer in a three-repo system (arxiv-archive -> idea-engine -> claude-code-factory). It autonomously:

1. Discovers promising arXiv papers from arxiv-archive
2. Gathers deep context: 7 related papers + 7 GitHub repos per candidate
3. Synthesizes monetizable product specifications via Claude through llm-router
4. Delivers specs to claude-code-factory for building

## For AI Agents Working on This Codebase

### Build & Test
```bash
make build    # compile to ./idea-engine
make test     # run tests with race detector
go vet ./...  # static analysis
```

### Architecture
- Single Go binary, cobra CLI
- Pipeline: discover -> research -> synthesize -> deliver
- All state in PostgreSQL (candidates + shipped_ideas tables)
- All LLM calls through llm-router HTTP API
- All paper data from arxiv-archive HTTP API

### Key Files to Understand
- `internal/types/types.go` — all shared data structures
- `internal/synthesize/synthesize.go` — the core prompt that generates product specs
- `internal/research/research.go` — orchestrates the 7+7 research pattern
- `main.go` — CLI commands and pipeline orchestration

### Environment
Requires: ARCHIVE_URL, LLM_ROUTER_URL, GITHUB_TOKEN, POSTGRES_URL

### Common Tasks
- **Add a new discovery signal**: Edit `internal/discover/discover.go`, add to `trendingTopics` or modify `scoreCandidate`
- **Modify the synthesis prompt**: Edit `buildPrompt()` in `internal/synthesize/synthesize.go`
- **Add a new CLI command**: Add to `main.go`, follow the existing cobra pattern
- **Change database schema**: Edit `ensureSchema()` in `internal/db/db.go`
