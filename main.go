package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/timholm/idea-engine/internal/api"
	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/db"
	"github.com/timholm/idea-engine/internal/deliver"
	"github.com/timholm/idea-engine/internal/discover"
	"github.com/timholm/idea-engine/internal/research"
	"github.com/timholm/idea-engine/internal/synthesize"
	"github.com/timholm/idea-engine/internal/types"
)

func main() {
	root := &cobra.Command{
		Use:   "idea-engine",
		Short: "Autonomous research agent that turns arXiv papers into monetizable product specs",
		Long: `idea-engine discovers promising research papers from arxiv-archive,
gathers deep context (7 related papers + 7 GitHub repos per concept),
and synthesizes monetizable product specifications via Claude through llm-router.

Output specs are consumed by claude-code-factory to build and ship products.`,
	}

	root.AddCommand(
		runCmd(),
		discoverCmd(),
		researchCmd(),
		synthesizeCmd(),
		deliverCmd(),
		listCmd(),
		statsCmd(),
		serveCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// runCmd executes the full pipeline: discover -> research -> synthesize -> deliver.
func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Full pipeline: discover candidates, research, synthesize specs, deliver to factory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			// Step 1: Discover candidates
			log.Println("=== Step 1: Discover candidates ===")
			disc := discover.New(cfg, database)
			candidates, err := disc.Run()
			if err != nil {
				return fmt.Errorf("discover: %w", err)
			}
			log.Printf("Discovered %d candidates", len(candidates))

			if len(candidates) == 0 {
				log.Println("No candidates found, exiting")
				return nil
			}

			// Step 2: Research each candidate
			log.Println("=== Step 2: Deep research ===")
			res := research.New(cfg, database)
			contexts := res.ResearchAll(candidates)
			log.Printf("Researched %d candidates successfully", len(contexts))

			if len(contexts) == 0 {
				log.Println("No research contexts produced, exiting")
				return nil
			}

			// Step 3: Synthesize product specs
			log.Println("=== Step 3: Synthesize product specs ===")
			syn := synthesize.New(cfg, database)
			specs := syn.SynthesizeAll(contexts)
			log.Printf("Synthesized %d product specs", len(specs))

			// Step 4: Deliver to factory
			log.Println("=== Step 4: Deliver to factory ===")
			del := deliver.New(cfg, database)
			delivered, err := del.DeliverAll()
			if err != nil {
				return fmt.Errorf("deliver: %w", err)
			}

			log.Printf("=== Pipeline complete: %d candidates -> %d researched -> %d specs -> %d delivered ===",
				len(candidates), len(contexts), len(specs), delivered)

			return nil
		},
	}
}

// discoverCmd finds candidate papers without researching them.
func discoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover",
		Short: "Find candidate papers from arxiv-archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			disc := discover.New(cfg, database)
			candidates, err := disc.Run()
			if err != nil {
				return err
			}

			fmt.Printf("Discovered %d candidates:\n\n", len(candidates))
			for i, c := range candidates {
				fmt.Printf("  %2d. [%.1f] %s\n      %s\n\n", i+1, c.Score, c.ArxivID, c.Title)
			}

			return nil
		},
	}
}

// researchCmd performs deep research on a single paper.
func researchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "research <arxiv_id>",
		Short: "Deep research one paper (7 related papers + 7 repos)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			arxivID := args[0]

			res := research.New(cfg, database)
			ctx, err := res.ResearchCandidate(arxivID)
			if err != nil {
				return err
			}

			fmt.Printf("Research for %s: %s\n\n", ctx.Candidate.ArxivID, ctx.Candidate.Title)

			fmt.Printf("Related Papers (%d):\n", len(ctx.Papers))
			for i, p := range ctx.Papers {
				fmt.Printf("  %d. [%.2f] %s — %s\n", i+1, p.Relevance, p.ArxivID, p.Title)
			}

			fmt.Printf("\nRelated Repos (%d):\n", len(ctx.Repos))
			for i, r := range ctx.Repos {
				fmt.Printf("  %d. [%.2f] %s (%d stars) — %s\n", i+1, r.Relevance, r.Name, r.Stars, r.Description)
			}

			return nil
		},
	}
}

// synthesizeCmd generates a product spec for a single paper.
func synthesizeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "synthesize <arxiv_id>",
		Short: "Generate a product spec for one paper (requires prior research)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			arxivID := args[0]

			// Check if research exists
			candidate, err := database.GetCandidate(arxivID)
			if err != nil {
				return fmt.Errorf("candidate %s not found — run 'idea-engine research %s' first", arxivID, arxivID)
			}

			if candidate.ResearchJSON == "" {
				// Need to do research first
				log.Printf("No cached research for %s, running research first...", arxivID)
				res := research.New(cfg, database)
				_, err := res.ResearchCandidate(arxivID)
				if err != nil {
					return fmt.Errorf("research: %w", err)
				}

				// Reload candidate with research
				candidate, err = database.GetCandidate(arxivID)
				if err != nil {
					return fmt.Errorf("reloading candidate: %w", err)
				}
			}

			// Parse the cached research context
			var ctx types.ResearchContext
			if err := json.Unmarshal([]byte(candidate.ResearchJSON), &ctx); err != nil {
				return fmt.Errorf("parsing cached research: %w", err)
			}

			syn := synthesize.New(cfg, database)
			spec, err := syn.SynthesizeSpec(&ctx)
			if err != nil {
				return err
			}

			// Pretty-print the spec
			data, _ := json.MarshalIndent(spec, "", "  ")
			fmt.Println(string(data))

			return nil
		},
	}
}

// deliverCmd pushes ready specs to the factory.
func deliverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deliver",
		Short: "Push synthesized specs to the factory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			del := deliver.New(cfg, database)
			delivered, err := del.DeliverAll()
			if err != nil {
				return err
			}

			fmt.Printf("Delivered %d specs\n", delivered)
			return nil
		},
	}
}

// listCmd shows pipeline status.
func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show pipeline status (candidates and their states)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			status, _ := cmd.Flags().GetString("status")
			limit, _ := cmd.Flags().GetInt("limit")

			candidates, err := database.ListCandidates(status, limit)
			if err != nil {
				return err
			}

			if len(candidates) == 0 {
				fmt.Println("No candidates found.")
				return nil
			}

			fmt.Printf("%-14s %-12s %6s  %s\n", "ARXIV ID", "STATUS", "SCORE", "TITLE")
			fmt.Println("─────────────────────────────────────────────────────────────────────────────")
			for _, c := range candidates {
				title := c.Title
				if len(title) > 50 {
					title = title[:47] + "..."
				}
				fmt.Printf("%-14s %-12s %6.1f  %s\n", c.ArxivID, c.Status, c.Score, title)
			}

			return nil
		},
	}

	cmd.Flags().StringP("status", "s", "", "Filter by status (pending, researching, synthesized, delivered, skipped)")
	cmd.Flags().IntP("limit", "n", 50, "Maximum number of candidates to show")

	return cmd
}

// statsCmd shows throughput and success rate.
func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show pipeline throughput and success rate",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			stats, err := database.Stats()
			if err != nil {
				return err
			}

			fmt.Println("idea-engine pipeline statistics")
			fmt.Println("───────────────────────────────")
			fmt.Printf("Total candidates:  %d\n", stats.TotalCandidates)
			fmt.Printf("  Pending:         %d\n", stats.Pending)
			fmt.Printf("  Researching:     %d\n", stats.Researching)
			fmt.Printf("  Synthesized:     %d\n", stats.Synthesized)
			fmt.Printf("  Delivered:       %d\n", stats.Delivered)
			fmt.Printf("  Skipped:         %d\n", stats.Skipped)
			fmt.Printf("Shipped ideas:     %d\n", stats.ShippedIdeas)
			fmt.Printf("Average score:     %.1f\n", stats.AvgScore)

			if stats.TotalCandidates > 0 {
				successRate := float64(stats.Delivered) / float64(stats.TotalCandidates) * 100
				fmt.Printf("Success rate:      %.1f%%\n", successRate)
			}

			return nil
		},
	}
}

// serveCmd starts the monitoring HTTP API.
func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the monitoring HTTP API",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			addr, _ := cmd.Flags().GetString("addr")

			srv := api.New(database)
			log.Printf("Starting idea-engine API on %s", addr)
			log.Printf("Endpoints: GET /status, GET /candidates, GET /specs, GET /stats")
			return http.ListenAndServe(addr, srv.Handler())
		},
	}

	cmd.Flags().String("addr", ":8090", "Listen address")

	return cmd
}

// bootstrap loads config and connects to the database.
func bootstrap() (*config.Config, *db.DB, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	database, err := db.New(cfg.PostgresURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to database: %w", err)
	}

	return cfg, database, nil
}
