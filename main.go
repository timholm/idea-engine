package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

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
		Short: "Autonomous research agent that fuses 7 arXiv papers into one product spec",
		Long: `idea-engine discovers promising research paper CLUSTERS from arxiv-archive,
groups 7 diverse papers per problem space (each with a different technique),
and synthesizes FUSION product specifications via Claude.

Each fusion product combines all 7 techniques into one unified tool that is
more powerful than any individual technique alone.

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

// runCmd executes the full pipeline: discover clusters -> research -> synthesize -> deliver.
func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Full pipeline: discover clusters, research techniques, synthesize fusion specs, deliver",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			// Step 1: Discover paper clusters
			log.Println("=== Step 1: Discover paper clusters ===")
			disc := discover.New(cfg, database)
			clusters, err := disc.Run()
			if err != nil {
				return fmt.Errorf("discover: %w", err)
			}
			log.Printf("Discovered %d fusion clusters", len(clusters))

			if len(clusters) == 0 {
				log.Println("No clusters found, exiting")
				return nil
			}

			// Step 2: Research each cluster (extract techniques + find repos)
			log.Println("=== Step 2: Fusion research ===")
			res := research.New(cfg, database)
			contexts := res.ResearchAll(clusters)
			log.Printf("Researched %d clusters successfully", len(contexts))

			if len(contexts) == 0 {
				log.Println("No research contexts produced, exiting")
				return nil
			}

			// Step 3: Synthesize fusion product specs
			log.Println("=== Step 3: Synthesize fusion specs ===")
			syn := synthesize.New(cfg, database)
			specs := syn.SynthesizeAll(contexts)
			log.Printf("Synthesized %d fusion product specs", len(specs))

			// Step 4: Deliver to factory
			log.Println("=== Step 4: Deliver to factory ===")
			del := deliver.New(cfg, database)
			delivered, err := del.DeliverAll()
			if err != nil {
				return fmt.Errorf("deliver: %w", err)
			}

			log.Printf("=== Pipeline complete: %d clusters -> %d researched -> %d specs -> %d delivered ===",
				len(clusters), len(contexts), len(specs), delivered)

			return nil
		},
	}
}

// discoverCmd finds paper clusters without researching them.
func discoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover",
		Short: "Find paper clusters from arxiv-archive (7 diverse papers per problem space)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			disc := discover.New(cfg, database)
			clusters, err := disc.Run()
			if err != nil {
				return err
			}

			fmt.Printf("Discovered %d fusion clusters:\n\n", len(clusters))
			for i, c := range clusters {
				fmt.Printf("  %2d. [%.1f] %s\n", i+1, c.Score, c.ProblemSpace)
				fmt.Printf("      Papers: %v\n\n", c.PaperIDs)
			}

			return nil
		},
	}
}

// researchCmd performs deep research on a single cluster.
func researchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "research <cluster_id>",
		Short: "Deep research one cluster (extract techniques from 7 papers + find repos)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			clusterID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid cluster ID: %w", err)
			}

			cluster, err := database.GetCluster(clusterID)
			if err != nil {
				return fmt.Errorf("cluster %d not found: %w", clusterID, err)
			}

			res := research.New(cfg, database)
			ctx, err := res.ResearchCluster(*cluster)
			if err != nil {
				return err
			}

			fmt.Printf("Fusion Research for: %s\n\n", ctx.Cluster.ProblemSpace)

			fmt.Printf("Techniques (%d):\n", len(ctx.Techniques))
			for i, t := range ctx.Techniques {
				fmt.Printf("  %d. [%s] %s\n     Technique: %s\n\n",
					i+1, t.ArxivID, t.Title, t.KeyTechnique)
			}

			fmt.Printf("Related Repos (%d):\n", len(ctx.Repos))
			for i, r := range ctx.Repos {
				fmt.Printf("  %d. [%.2f] %s (%d stars) — %s\n",
					i+1, r.Relevance, r.Name, r.Stars, r.Description)
			}

			return nil
		},
	}
}

// synthesizeCmd generates a fusion product spec for a cluster.
func synthesizeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "synthesize <cluster_id>",
		Short: "Generate a fusion product spec for one cluster (requires prior research)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			clusterID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid cluster ID: %w", err)
			}

			cluster, err := database.GetCluster(clusterID)
			if err != nil {
				return fmt.Errorf("cluster %d not found: %w", clusterID, err)
			}

			if cluster.ResearchJSON == "" {
				// Need to do research first
				log.Printf("No cached research for cluster %d, running research first...", clusterID)
				res := research.New(cfg, database)
				_, err := res.ResearchCluster(*cluster)
				if err != nil {
					return fmt.Errorf("research: %w", err)
				}

				// Reload cluster with research
				cluster, err = database.GetCluster(clusterID)
				if err != nil {
					return fmt.Errorf("reloading cluster: %w", err)
				}
			}

			// Parse the cached research context
			var ctx types.FusionResearchContext
			if err := json.Unmarshal([]byte(cluster.ResearchJSON), &ctx); err != nil {
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
		Short: "Push synthesized fusion specs to the factory",
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

			fmt.Printf("Delivered %d fusion specs\n", delivered)
			return nil
		},
	}
}

// listCmd shows pipeline status.
func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show pipeline status (clusters and their states)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, database, err := bootstrap()
			if err != nil {
				return err
			}
			defer database.Close()

			status, _ := cmd.Flags().GetString("status")
			limit, _ := cmd.Flags().GetInt("limit")

			clusters, err := database.ListClusters(status, limit)
			if err != nil {
				return err
			}

			if len(clusters) == 0 {
				fmt.Println("No clusters found.")
				return nil
			}

			fmt.Printf("%-4s %-12s %6s  %-30s  %s\n", "ID", "STATUS", "SCORE", "PROBLEM SPACE", "PAPERS")
			fmt.Println(strings.Repeat("-", 90))
			for _, c := range clusters {
				space := c.ProblemSpace
				if len(space) > 30 {
					space = space[:27] + "..."
				}
				papers := fmt.Sprintf("%d papers", len(c.PaperIDs))
				fmt.Printf("%-4d %-12s %6.1f  %-30s  %s\n", c.ID, c.Status, c.Score, space, papers)
			}

			return nil
		},
	}

	cmd.Flags().StringP("status", "s", "", "Filter by status (pending, researching, synthesized, delivered, skipped)")
	cmd.Flags().IntP("limit", "n", 50, "Maximum number of clusters to show")

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

			fmt.Println("idea-engine fusion pipeline statistics")
			fmt.Println(strings.Repeat("-", 40))
			fmt.Printf("Total clusters:    %d\n", stats.TotalClusters)
			fmt.Printf("  Pending:         %d\n", stats.Pending)
			fmt.Printf("  Researching:     %d\n", stats.Researching)
			fmt.Printf("  Synthesized:     %d\n", stats.Synthesized)
			fmt.Printf("  Delivered:       %d\n", stats.Delivered)
			fmt.Printf("  Skipped:         %d\n", stats.Skipped)
			fmt.Printf("Shipped ideas:     %d\n", stats.ShippedIdeas)
			fmt.Printf("Average score:     %.1f\n", stats.AvgScore)

			if stats.TotalClusters > 0 {
				successRate := float64(stats.Delivered) / float64(stats.TotalClusters) * 100
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
			log.Printf("Starting idea-engine fusion API on %s", addr)
			log.Printf("Endpoints: GET /status, GET /clusters, GET /specs, GET /stats")
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
