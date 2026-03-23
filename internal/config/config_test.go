package config

import (
	"os"
	"testing"
)

func TestLoad_AllRequired(t *testing.T) {
	// Set all required env vars
	setEnvs(t, map[string]string{
		"ARCHIVE_URL":   "http://localhost:9090",
		"LLM_ROUTER_URL": "http://localhost:8080",
		"GITHUB_TOKEN":  "ghp_test123",
		"POSTGRES_URL":  "postgres://localhost/ideaengine",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.ArchiveURL != "http://localhost:9090" {
		t.Errorf("ArchiveURL = %q, want %q", cfg.ArchiveURL, "http://localhost:9090")
	}
	if cfg.CandidatesPerRun != 30 {
		t.Errorf("CandidatesPerRun = %d, want 30", cfg.CandidatesPerRun)
	}
	if cfg.SpecsPerRun != 15 {
		t.Errorf("SpecsPerRun = %d, want 15", cfg.SpecsPerRun)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	tests := []struct {
		name    string
		envs    map[string]string
		wantErr string
	}{
		{
			name:    "missing ARCHIVE_URL",
			envs:    map[string]string{"LLM_ROUTER_URL": "x", "GITHUB_TOKEN": "x", "POSTGRES_URL": "x"},
			wantErr: "ARCHIVE_URL is required (arxiv-archive HTTP API base URL)",
		},
		{
			name:    "missing LLM_ROUTER_URL",
			envs:    map[string]string{"ARCHIVE_URL": "x", "GITHUB_TOKEN": "x", "POSTGRES_URL": "x"},
			wantErr: "LLM_ROUTER_URL is required (llm-router base URL)",
		},
		{
			name:    "missing GITHUB_TOKEN",
			envs:    map[string]string{"ARCHIVE_URL": "x", "LLM_ROUTER_URL": "x", "POSTGRES_URL": "x"},
			wantErr: "GITHUB_TOKEN is required (GitHub personal access token)",
		},
		{
			name:    "missing POSTGRES_URL",
			envs:    map[string]string{"ARCHIVE_URL": "x", "LLM_ROUTER_URL": "x", "GITHUB_TOKEN": "x"},
			wantErr: "POSTGRES_URL is required (PostgreSQL connection string)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvs(t)
			setEnvs(t, tt.envs)

			_, err := Load()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoad_CustomLimits(t *testing.T) {
	setEnvs(t, map[string]string{
		"ARCHIVE_URL":       "http://localhost:9090",
		"LLM_ROUTER_URL":   "http://localhost:8080",
		"GITHUB_TOKEN":     "ghp_test123",
		"POSTGRES_URL":     "postgres://localhost/ideaengine",
		"CANDIDATES_PER_RUN": "50",
		"SPECS_PER_RUN":      "25",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.CandidatesPerRun != 50 {
		t.Errorf("CandidatesPerRun = %d, want 50", cfg.CandidatesPerRun)
	}
	if cfg.SpecsPerRun != 25 {
		t.Errorf("SpecsPerRun = %d, want 25", cfg.SpecsPerRun)
	}
}

func TestLoad_InvalidLimits(t *testing.T) {
	setEnvs(t, map[string]string{
		"ARCHIVE_URL":       "http://localhost:9090",
		"LLM_ROUTER_URL":   "http://localhost:8080",
		"GITHUB_TOKEN":     "ghp_test123",
		"POSTGRES_URL":     "postgres://localhost/ideaengine",
		"CANDIDATES_PER_RUN": "not-a-number",
	})

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid CANDIDATES_PER_RUN, got nil")
	}
}

func setEnvs(t *testing.T, envs map[string]string) {
	t.Helper()
	for k, v := range envs {
		t.Setenv(k, v)
	}
}

func clearEnvs(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ARCHIVE_URL", "LLM_ROUTER_URL", "GITHUB_TOKEN", "POSTGRES_URL",
		"FACTORY_SPECS_DIR", "CANDIDATES_PER_RUN", "SPECS_PER_RUN",
	} {
		os.Unsetenv(key)
	}
}
