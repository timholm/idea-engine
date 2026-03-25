# Stage 1: Builder
FROM golang:1.23-bookworm AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o idea-engine .

# Stage 2: Runtime
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 22 via nodesource (needed for claude CLI)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install Claude Code CLI (idea-engine uses `claude -p` for synthesis)
RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /build/idea-engine /usr/local/bin/idea-engine

ENTRYPOINT ["idea-engine"]
