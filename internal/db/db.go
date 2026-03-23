// Package db manages PostgreSQL connections and schema for idea-engine state.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/timholm/idea-engine/internal/types"
)

// DB wraps a PostgreSQL connection with idea-engine-specific operations.
type DB struct {
	conn *sql.DB
}

// New opens a PostgreSQL connection and ensures the schema exists.
func New(postgresURL string) (*DB, error) {
	conn, err := sql.Open("postgres", postgresURL)
	if err != nil {
		return nil, fmt.Errorf("opening postgres: %w", err)
	}

	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.ensureSchema(); err != nil {
		return nil, fmt.Errorf("ensuring schema: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) ensureSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS candidates (
		id              SERIAL PRIMARY KEY,
		arxiv_id        TEXT NOT NULL,
		title           TEXT,
		discovered_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		status          TEXT DEFAULT 'pending',
		score           FLOAT DEFAULT 0,
		research_json   TEXT,
		spec_json       TEXT,
		delivered_at    TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_candidates_arxiv_id ON candidates(arxiv_id);
	CREATE INDEX IF NOT EXISTS idx_candidates_status ON candidates(status);

	CREATE TABLE IF NOT EXISTS shipped_ideas (
		name        TEXT PRIMARY KEY,
		arxiv_id    TEXT,
		shipped_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.conn.Exec(schema)
	return err
}

// InsertCandidate adds a new candidate paper to the database. Returns the new ID.
func (db *DB) InsertCandidate(arxivID, title string, score float64) (int, error) {
	// Skip if already exists
	var existing int
	err := db.conn.QueryRow("SELECT id FROM candidates WHERE arxiv_id = $1", arxivID).Scan(&existing)
	if err == nil {
		return existing, nil // already tracked
	}

	var id int
	err = db.conn.QueryRow(
		"INSERT INTO candidates (arxiv_id, title, score) VALUES ($1, $2, $3) RETURNING id",
		arxivID, title, score,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting candidate %s: %w", arxivID, err)
	}
	return id, nil
}

// UpdateStatus sets the status of a candidate.
func (db *DB) UpdateStatus(arxivID, status string) error {
	_, err := db.conn.Exec("UPDATE candidates SET status = $1 WHERE arxiv_id = $2", status, arxivID)
	return err
}

// SaveResearch stores the research context JSON for a candidate.
func (db *DB) SaveResearch(arxivID string, research *types.ResearchContext) error {
	data, err := json.Marshal(research)
	if err != nil {
		return fmt.Errorf("marshaling research: %w", err)
	}
	_, err = db.conn.Exec(
		"UPDATE candidates SET research_json = $1, status = 'researching' WHERE arxiv_id = $2",
		string(data), arxivID,
	)
	return err
}

// SaveSpec stores the generated product spec JSON for a candidate.
func (db *DB) SaveSpec(arxivID string, spec *types.ProductSpec) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}
	_, err = db.conn.Exec(
		"UPDATE candidates SET spec_json = $1, status = 'synthesized' WHERE arxiv_id = $2",
		string(data), arxivID,
	)
	return err
}

// MarkDelivered marks a candidate as delivered and records the timestamp.
func (db *DB) MarkDelivered(arxivID string) error {
	_, err := db.conn.Exec(
		"UPDATE candidates SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP WHERE arxiv_id = $1",
		arxivID,
	)
	return err
}

// MarkSkipped marks a candidate as skipped (failed quality gate or error).
func (db *DB) MarkSkipped(arxivID string) error {
	_, err := db.conn.Exec(
		"UPDATE candidates SET status = 'skipped' WHERE arxiv_id = $1",
		arxivID,
	)
	return err
}

// GetCandidatesByStatus returns candidates matching the given status, ordered by score descending.
func (db *DB) GetCandidatesByStatus(status string, limit int) ([]types.Candidate, error) {
	rows, err := db.conn.Query(
		"SELECT id, arxiv_id, title, status, score, discovered_at FROM candidates WHERE status = $1 ORDER BY score DESC LIMIT $2",
		status, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []types.Candidate
	for rows.Next() {
		var c types.Candidate
		if err := rows.Scan(&c.ID, &c.ArxivID, &c.Title, &c.Status, &c.Score, &c.DiscoveredAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// GetCandidate returns a single candidate by arxiv ID, including research and spec JSON.
func (db *DB) GetCandidate(arxivID string) (*types.Candidate, error) {
	var c types.Candidate
	var researchJSON, specJSON sql.NullString
	var deliveredAt sql.NullTime

	err := db.conn.QueryRow(
		"SELECT id, arxiv_id, title, status, score, research_json, spec_json, discovered_at, delivered_at FROM candidates WHERE arxiv_id = $1",
		arxivID,
	).Scan(&c.ID, &c.ArxivID, &c.Title, &c.Status, &c.Score, &researchJSON, &specJSON, &c.DiscoveredAt, &deliveredAt)
	if err != nil {
		return nil, err
	}

	if researchJSON.Valid {
		c.ResearchJSON = researchJSON.String
	}
	if specJSON.Valid {
		c.SpecJSON = specJSON.String
	}
	if deliveredAt.Valid {
		c.DeliveredAt = &deliveredAt.Time
	}

	return &c, nil
}

// GetSynthesizedSpecs returns all candidates with status 'synthesized' that have specs ready for delivery.
func (db *DB) GetSynthesizedSpecs(limit int) ([]types.Candidate, error) {
	rows, err := db.conn.Query(
		"SELECT id, arxiv_id, title, status, score, spec_json, discovered_at FROM candidates WHERE status = 'synthesized' AND spec_json IS NOT NULL ORDER BY score DESC LIMIT $1",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []types.Candidate
	for rows.Next() {
		var c types.Candidate
		var specJSON sql.NullString
		if err := rows.Scan(&c.ID, &c.ArxivID, &c.Title, &c.Status, &c.Score, &specJSON, &c.DiscoveredAt); err != nil {
			return nil, err
		}
		if specJSON.Valid {
			c.SpecJSON = specJSON.String
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// RecordShippedIdea tracks a product name as shipped to prevent duplicates.
func (db *DB) RecordShippedIdea(name, arxivID string) error {
	_, err := db.conn.Exec(
		"INSERT INTO shipped_ideas (name, arxiv_id) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING",
		name, arxivID,
	)
	return err
}

// IsIdeaShipped checks if a product name has already been shipped.
func (db *DB) IsIdeaShipped(name string) (bool, error) {
	var exists bool
	err := db.conn.QueryRow("SELECT EXISTS(SELECT 1 FROM shipped_ideas WHERE name = $1)", name).Scan(&exists)
	return exists, err
}

// Stats returns aggregate pipeline statistics.
func (db *DB) Stats() (*types.Stats, error) {
	s := &types.Stats{}

	err := db.conn.QueryRow("SELECT COUNT(*) FROM candidates").Scan(&s.TotalCandidates)
	if err != nil {
		return nil, err
	}

	for _, status := range []struct {
		name string
		dest *int
	}{
		{"pending", &s.Pending},
		{"researching", &s.Researching},
		{"synthesized", &s.Synthesized},
		{"delivered", &s.Delivered},
		{"skipped", &s.Skipped},
	} {
		err := db.conn.QueryRow("SELECT COUNT(*) FROM candidates WHERE status = $1", status.name).Scan(status.dest)
		if err != nil {
			return nil, err
		}
	}

	err = db.conn.QueryRow("SELECT COUNT(*) FROM shipped_ideas").Scan(&s.ShippedIdeas)
	if err != nil {
		return nil, err
	}

	err = db.conn.QueryRow("SELECT COALESCE(AVG(score), 0) FROM candidates WHERE score > 0").Scan(&s.AvgScore)
	if err != nil {
		return nil, err
	}

	return s, nil
}

// ListCandidates returns recent candidates with optional status filter.
func (db *DB) ListCandidates(status string, limit int) ([]types.Candidate, error) {
	var rows *sql.Rows
	var err error

	if status != "" {
		rows, err = db.conn.Query(
			"SELECT id, arxiv_id, title, status, score, discovered_at FROM candidates WHERE status = $1 ORDER BY discovered_at DESC LIMIT $2",
			status, limit,
		)
	} else {
		rows, err = db.conn.Query(
			"SELECT id, arxiv_id, title, status, score, discovered_at FROM candidates ORDER BY discovered_at DESC LIMIT $1",
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []types.Candidate
	for rows.Next() {
		var c types.Candidate
		if err := rows.Scan(&c.ID, &c.ArxivID, &c.Title, &c.Status, &c.Score, &c.DiscoveredAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}
