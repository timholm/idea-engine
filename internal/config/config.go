// Package config loads and validates environment-based configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for idea-engine.
type Config struct {
	ArchiveURL      string // arxiv-archive HTTP API base URL
	LLMRouterURL    string // llm-router base URL for all Claude calls
	GitHubToken     string // GitHub personal access token
	PostgresURL     string // PostgreSQL connection string
	FactorySpecsDir string // directory to write specs for factory (optional)
	CandidatesPerRun int   // max candidates to discover per run
	SpecsPerRun      int   // max specs to output per run
}

// Load reads configuration from environment variables and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		ArchiveURL:       os.Getenv("ARCHIVE_URL"),
		LLMRouterURL:     os.Getenv("LLM_ROUTER_URL"),
		GitHubToken:      os.Getenv("GITHUB_TOKEN"),
		PostgresURL:      os.Getenv("POSTGRES_URL"),
		FactorySpecsDir:  os.Getenv("FACTORY_SPECS_DIR"),
		CandidatesPerRun: 30,
		SpecsPerRun:      15,
	}

	if v := os.Getenv("CANDIDATES_PER_RUN"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("CANDIDATES_PER_RUN must be an integer: %w", err)
		}
		cfg.CandidatesPerRun = n
	}

	if v := os.Getenv("SPECS_PER_RUN"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("SPECS_PER_RUN must be an integer: %w", err)
		}
		cfg.SpecsPerRun = n
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.ArchiveURL == "" {
		return fmt.Errorf("ARCHIVE_URL is required (arxiv-archive HTTP API base URL)")
	}
	if c.LLMRouterURL == "" {
		return fmt.Errorf("LLM_ROUTER_URL is required (llm-router base URL)")
	}
	if c.GitHubToken == "" {
		return fmt.Errorf("GITHUB_TOKEN is required (GitHub personal access token)")
	}
	if c.PostgresURL == "" {
		return fmt.Errorf("POSTGRES_URL is required (PostgreSQL connection string)")
	}
	return nil
}
