package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pedromol/combobreak/config"
	"github.com/pedromol/combobreak/omniroute"
)

func main() {
	cfg, err := config.LoadFromEnvAndFlags(os.Args[1:])
	if err != nil {
		log.Fatalf("[FATAL] Configuration error: %v", err)
	}

	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}

	client := omniroute.NewClient(cfg.BaseURL, cfg.APIKey, httpClient)
	modelFilter := omniroute.NewModelFilter(cfg.ComboName)

	printBanner(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initial sync
	if err := runSync(ctx, cfg, client, modelFilter); err != nil {
		log.Printf("[ERROR] Sync failed: %v", err)
		if cfg.Interval == 0 {
			os.Exit(1)
		}
	}

	// If no recurring interval requested, exit cleanly
	if cfg.Interval <= 0 {
		return
	}

	log.Printf("[INFO] Watching OmniRoute models every %v. Press Ctrl+C to stop.", cfg.Interval)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[INFO] Shutting down gracefully...")
			return
		case <-ticker.C:
			log.Println("[INFO] Running scheduled sync...")
			if err := runSync(ctx, cfg, client, modelFilter); err != nil {
				log.Printf("[ERROR] Scheduled sync failed: %v", err)
			}
		}
	}
}

func runSync(ctx context.Context, cfg *config.Config, client *omniroute.Client, filter *omniroute.ModelFilter) error {
	log.Printf("[INFO] Fetching models from %s/v1/models...", cfg.BaseURL)
	models, err := client.GetModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get models: %w", err)
	}
	log.Printf("[INFO] Retrieved %d total models from OmniRoute catalog.", len(models))

	matchedModels, allResults := filter.Filter(models)

	if cfg.Verbose {
		log.Println("[DEBUG] Evaluation details for all models:")
		for _, res := range allResults {
			if res.Passed {
				log.Printf("  [MATCH] %s | tool_call: %t | reasoning: %t | free: %s",
					res.Model.ID, res.HasToolCall, res.HasReason, res.FreeReason)
			} else {
				log.Printf("  [SKIP]  %s | reason(s): %v", res.Model.ID, res.RejectNotes)
			}
		}
	}

	log.Printf("[INFO] Found %d matching models (Tool Calling + Reasoning + Free):", len(matchedModels))
	for i, m := range matchedModels {
		log.Printf("  [%02d] %s (owned_by: %s)", i+1, m.ID, m.OwnedBy)
	}

	if len(matchedModels) == 0 {
		log.Printf("[WARN] No models matched the criteria. Skipping combo creation/update.")
		return nil
	}

	var steps []omniroute.ComboStep
	for _, m := range matchedModels {
		steps = append(steps, omniroute.ComboStep{
			Model: m.ID,
		})
	}

	combo := omniroute.Combo{
		ID:          cfg.ComboName,
		Name:        cfg.ComboName,
		Strategy:    cfg.Strategy,
		Description: "Free models with tool calling and reasoning capabilities (managed by combobreak)",
		Models:      steps,
	}

	if cfg.DryRun {
		log.Printf("[INFO] [DRY RUN] Would create/update combo %q with %d models. Payload:", cfg.ComboName, len(steps))
		for _, step := range steps {
			log.Printf("   - %s", step.Model)
		}
		return nil
	}

	log.Printf("[INFO] Calling /api/combos to create/update combo %q...", cfg.ComboName)
	action, err := client.UpsertCombo(ctx, combo)
	if err != nil {
		return fmt.Errorf("failed to upsert combo: %w", err)
	}

	log.Printf("[SUCCESS] Successfully %s combo %q with %d models (strategy: %s).",
		action, cfg.ComboName, len(steps), cfg.Strategy)

	return nil
}

func printBanner(cfg *config.Config) {
	maskedKey := "not set"
	if cfg.APIKey != "" {
		if len(cfg.APIKey) > 8 {
			maskedKey = cfg.APIKey[:4] + "..." + cfg.APIKey[len(cfg.APIKey)-4:]
		} else {
			maskedKey = "****"
		}
	}

	fmt.Println("======================================================")
	fmt.Println("   COMBOBREAK - OmniRoute Free Reasoning Combo Sync   ")
	fmt.Println("======================================================")
	fmt.Printf(" Base URL    : %s\n", cfg.BaseURL)
	fmt.Printf(" API Key     : %s\n", maskedKey)
	fmt.Printf(" Combo Name  : %s\n", cfg.ComboName)
	fmt.Printf(" Strategy    : %s\n", cfg.Strategy)
	fmt.Printf(" Dry Run     : %t\n", cfg.DryRun)
	fmt.Printf(" Verbose     : %t\n", cfg.Verbose)
	if cfg.Interval > 0 {
		fmt.Printf(" Interval    : %v\n", cfg.Interval)
	} else {
		fmt.Println(" Mode        : Run once")
	}
	fmt.Println("======================================================")
}
