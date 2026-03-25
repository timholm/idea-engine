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
	CREATE TABLE IF NOT EXISTS clusters (
		id              SERIAL PRIMARY KEY,
		problem_space   TEXT NOT NULL,
		paper_ids       TEXT NOT NULL,
		score           FLOAT DEFAULT 0,
		status          TEXT DEFAULT 'pending',
		research_json   TEXT,
		spec_json       TEXT,
		created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		delivered_at    TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_clusters_status ON clusters(status);
	CREATE INDEX IF NOT EXISTS idx_clusters_problem_space ON clusters(problem_space);

	CREATE TABLE IF NOT EXISTS shipped_ideas (
		name        TEXT PRIMARY KEY,
		cluster_id  INTEGER,
		shipped_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.conn.Exec(schema)
	return err
}

// InsertCluster adds a new paper cluster to the database. Returns the new ID.
func (db *DB) InsertCluster(problemSpace string, paperIDs []string, score float64) (int, error) {
	idsJSON, err := json.Marshal(paperIDs)
	if err != nil {
		return 0, fmt.Errorf("marshaling paper IDs: %w", err)
	}

	// Skip if cluster with same problem space already exists and is pending
	var existing int
	err = db.conn.QueryRow(
		"SELECT id FROM clusters WHERE problem_space = $1 AND status = 'pending'",
		problemSpace,
	).Scan(&existing)
	if err == nil {
		return existing, nil
	}

	var id int
	err = db.conn.QueryRow(
		"INSERT INTO clusters (problem_space, paper_ids, score) VALUES ($1, $2, $3) RETURNING id",
		problemSpace, string(idsJSON), score,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting cluster '%s': %w", problemSpace, err)
	}
	return id, nil
}

// UpdateClusterStatus sets the status of a cluster.
func (db *DB) UpdateClusterStatus(id int, status string) error {
	_, err := db.conn.Exec("UPDATE clusters SET status = $1 WHERE id = $2", status, id)
	return err
}

// SaveClusterResearch stores the research context JSON for a cluster.
func (db *DB) SaveClusterResearch(id int, researchJSON string) error {
	_, err := db.conn.Exec(
		"UPDATE clusters SET research_json = $1, status = 'researching' WHERE id = $2",
		researchJSON, id,
	)
	return err
}

// SaveClusterSpec stores the generated product spec JSON for a cluster.
func (db *DB) SaveClusterSpec(id int, spec *types.ProductSpec) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}
	_, err = db.conn.Exec(
		"UPDATE clusters SET spec_json = $1, status = 'synthesized' WHERE id = $2",
		string(data), id,
	)
	return err
}

// MarkClusterDelivered marks a cluster as delivered and records the timestamp.
func (db *DB) MarkClusterDelivered(id int) error {
	_, err := db.conn.Exec(
		"UPDATE clusters SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP WHERE id = $1",
		id,
	)
	return err
}

// MarkClusterSkipped marks a cluster as skipped.
func (db *DB) MarkClusterSkipped(id int) error {
	_, err := db.conn.Exec(
		"UPDATE clusters SET status = 'skipped' WHERE id = $1",
		id,
	)
	return err
}

// GetCluster returns a single cluster by ID, including research and spec JSON.
func (db *DB) GetCluster(id int) (*types.PaperCluster, error) {
	var c types.PaperCluster
	var paperIDsJSON string
	var researchJSON, specJSON sql.NullString
	var deliveredAt sql.NullTime

	err := db.conn.QueryRow(
		"SELECT id, problem_space, paper_ids, score, status, research_json, spec_json, created_at, delivered_at FROM clusters WHERE id = $1",
		id,
	).Scan(&c.ID, &c.ProblemSpace, &paperIDsJSON, &c.Score, &c.Status, &researchJSON, &specJSON, &c.CreatedAt, &deliveredAt)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(paperIDsJSON), &c.PaperIDs); err != nil {
		return nil, fmt.Errorf("parsing paper IDs: %w", err)
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

// GetSynthesizedClusters returns all clusters with status 'synthesized' that have specs ready for delivery.
func (db *DB) GetSynthesizedClusters(limit int) ([]types.PaperCluster, error) {
	rows, err := db.conn.Query(
		"SELECT id, problem_space, paper_ids, score, status, spec_json, created_at FROM clusters WHERE status = 'synthesized' AND spec_json IS NOT NULL ORDER BY score DESC LIMIT $1",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []types.PaperCluster
	for rows.Next() {
		var c types.PaperCluster
		var paperIDsJSON string
		var specJSON sql.NullString
		if err := rows.Scan(&c.ID, &c.ProblemSpace, &paperIDsJSON, &c.Score, &c.Status, &specJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(paperIDsJSON), &c.PaperIDs); err != nil {
			continue
		}
		if specJSON.Valid {
			c.SpecJSON = specJSON.String
		}
		clusters = append(clusters, c)
	}
	return clusters, rows.Err()
}

// RecordShippedIdea tracks a product name as shipped to prevent duplicates.
func (db *DB) RecordShippedIdea(name string, clusterID int) error {
	_, err := db.conn.Exec(
		"INSERT INTO shipped_ideas (name, cluster_id) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING",
		name, clusterID,
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

	err := db.conn.QueryRow("SELECT COUNT(*) FROM clusters").Scan(&s.TotalClusters)
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
		err := db.conn.QueryRow("SELECT COUNT(*) FROM clusters WHERE status = $1", status.name).Scan(status.dest)
		if err != nil {
			return nil, err
		}
	}

	err = db.conn.QueryRow("SELECT COUNT(*) FROM shipped_ideas").Scan(&s.ShippedIdeas)
	if err != nil {
		return nil, err
	}

	err = db.conn.QueryRow("SELECT COALESCE(AVG(score), 0) FROM clusters WHERE score > 0").Scan(&s.AvgScore)
	if err != nil {
		return nil, err
	}

	return s, nil
}

// ListClusters returns recent clusters with optional status filter.
func (db *DB) ListClusters(status string, limit int) ([]types.PaperCluster, error) {
	var rows *sql.Rows
	var err error

	if status != "" {
		rows, err = db.conn.Query(
			"SELECT id, problem_space, paper_ids, score, status, created_at FROM clusters WHERE status = $1 ORDER BY created_at DESC LIMIT $2",
			status, limit,
		)
	} else {
		rows, err = db.conn.Query(
			"SELECT id, problem_space, paper_ids, score, status, created_at FROM clusters ORDER BY created_at DESC LIMIT $1",
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []types.PaperCluster
	for rows.Next() {
		var c types.PaperCluster
		var paperIDsJSON string
		if err := rows.Scan(&c.ID, &c.ProblemSpace, &paperIDsJSON, &c.Score, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(paperIDsJSON), &c.PaperIDs); err != nil {
			continue
		}
		clusters = append(clusters, c)
	}
	return clusters, rows.Err()
}
