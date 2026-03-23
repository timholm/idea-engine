// Package deliver pushes generated product specs to the factory for building.
package deliver

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/types"
)

// Deliverer pushes product specs to the factory.
type Deliverer struct {
	cfg *config.Config
	db  *db.DB
}

// New creates a Deliverer.
func New(cfg *config.Config, database *db.DB) *Deliverer {
	return &Deliverer{
		cfg: cfg,
		db:  database,
	}
}

// DeliverAll delivers all synthesized specs that haven't been delivered yet.
func (d *Deliverer) DeliverAll() (int, error) {
	candidates, err := d.db.GetSynthesizedSpecs(d.cfg.SpecsPerRun)
	if err != nil {
		return 0, fmt.Errorf("fetching synthesized specs: %w", err)
	}

	if len(candidates) == 0 {
		log.Printf("[deliver] no synthesized specs to deliver")
		return 0, nil
	}

	delivered := 0
	for _, c := range candidates {
		var spec types.ProductSpec
		if err := json.Unmarshal([]byte(c.SpecJSON), &spec); err != nil {
			log.Printf("[deliver] warning: invalid spec JSON for %s: %v", c.ArxivID, err)
			continue
		}

		// Check if already shipped
		alreadyShipped, err := d.db.IsIdeaShipped(spec.Name)
		if err != nil {
			log.Printf("[deliver] warning: shipped check failed for %s: %v", spec.Name, err)
		}
		if alreadyShipped {
			log.Printf("[deliver] skipping %s: already shipped", spec.Name)
			if err := d.db.MarkSkipped(c.ArxivID); err != nil {
				log.Printf("[deliver] warning: failed to mark %s as skipped: %v", c.ArxivID, err)
			}
			continue
		}

		// Deliver the spec
		if err := d.deliverSpec(&spec); err != nil {
			log.Printf("[deliver] warning: failed to deliver %s: %v", spec.Name, err)
			continue
		}

		// Mark as delivered in the database
		if err := d.db.MarkDelivered(c.ArxivID); err != nil {
			log.Printf("[deliver] warning: failed to mark %s as delivered: %v", c.ArxivID, err)
		}

		// Record as shipped
		if err := d.db.RecordShippedIdea(spec.Name, c.ArxivID); err != nil {
			log.Printf("[deliver] warning: failed to record shipped idea %s: %v", spec.Name, err)
		}

		delivered++
		log.Printf("[deliver] delivered: %s (%s)", spec.Name, c.ArxivID)
	}

	log.Printf("[deliver] delivered %d/%d specs", delivered, len(candidates))
	return delivered, nil
}

// deliverSpec writes a product spec to the factory specs directory.
func (d *Deliverer) deliverSpec(spec *types.ProductSpec) error {
	dir := d.cfg.FactorySpecsDir
	if dir == "" {
		dir = "specs" // default to local specs directory
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating specs directory: %w", err)
	}

	// Use timestamp + name for unique filename
	ts := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s-%s.json", ts, sanitizeFilename(spec.Name))
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing spec to %s: %w", path, err)
	}

	log.Printf("[deliver] wrote spec to %s", path)
	return nil
}

// sanitizeFilename makes a string safe for use as a filename.
func sanitizeFilename(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
