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
	coordinator := omniroute.NewSyncCoordinator(client, modelFilter)

	printBanner(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initial sync
	if err := runSync(ctx, cfg, coordinator); err != nil {
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
			if err := runSync(ctx, cfg, coordinator); err != nil {
				log.Printf("[ERROR] Scheduled sync failed: %v", err)
			}
		}
	}
}

func runSync(ctx context.Context, cfg *config.Config, coordinator *omniroute.SyncCoordinator) error {
	log.Printf("[INFO] Starting synchronization against OmniRoute at %s...", cfg.BaseURL)

	report, err := coordinator.Sync(ctx, cfg.ComboName, cfg.Strategy, cfg.DryRun, cfg.Verbose)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	log.Printf("[INFO] Sync finished with action %q: %d providers checked (%d passed), %d models tested (%d approved, %d failed).",
		report.Action,
		report.TotalProvidersChecked,
		report.ProvidersPassed,
		report.TotalModelsTested,
		len(report.ModelsApproved),
		len(report.ModelsFailed),
	)

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
	fmt.Printf(" Timeout     : %v\n", cfg.Timeout)
	fmt.Printf(" Dry Run     : %t\n", cfg.DryRun)
	fmt.Printf(" Verbose     : %t\n", cfg.Verbose)
	if cfg.Interval > 0 {
		fmt.Printf(" Interval    : %v\n", cfg.Interval)
	} else {
		fmt.Println(" Mode        : Run once")
	}
	fmt.Println("======================================================")
}
