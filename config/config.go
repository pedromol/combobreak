package config

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds the runtime configuration for combobreak
type Config struct {
	BaseURL   string
	APIKey    string
	ComboName string
	Strategy  string
	DryRun    bool
	Verbose   bool
	Interval  time.Duration
	Timeout   time.Duration
}

// LoadFromEnvAndFlags loads configurations prioritizing flags, then environment variables, with fallback defaults
func LoadFromEnvAndFlags(args []string) (*Config, error) {
	// Try loading .env if present
	_ = loadDotEnv(".env")

	cfg := &Config{
		BaseURL:   getEnvOrDefault("OMNIROUTE_BASE_URL", "http://localhost:20128"),
		APIKey:    os.Getenv("OMNIROUTE_API_KEY"),
		ComboName: getEnvOrDefault("COMBO_NAME", "combobreak"),
		Strategy:  getEnvOrDefault("COMBO_STRATEGY", "priority"),
		Timeout:   30 * time.Second,
	}

	fs := flag.NewFlagSet("combobreak", flag.ContinueOnError)
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "OmniRoute base URL (env: OMNIROUTE_BASE_URL)")
	fs.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "OmniRoute API Key (env: OMNIROUTE_API_KEY)")
	fs.StringVar(&cfg.ComboName, "combo-name", cfg.ComboName, "Name of the combo to create or update")
	fs.StringVar(&cfg.Strategy, "strategy", cfg.Strategy, "Combo routing strategy (priority, round-robin, etc.)")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "Preview matching models without modifying combos in OmniRoute")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging of model evaluations")
	fs.DurationVar(&cfg.Interval, "interval", 0, "Periodic sync interval (e.g. 1h, 30m). If 0, runs once and exits")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP client timeout")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("OMNIROUTE_BASE_URL is required")
	}

	if cfg.ComboName == "" {
		cfg.ComboName = "combobreak"
	}
	if cfg.Strategy == "" {
		cfg.Strategy = "priority"
	}

	return cfg, nil
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// loadDotEnv loads simple KEY=VALUE pairs from a .env file if it exists
func loadDotEnv(filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			v = strings.Trim(v, `"'`)
			// Only set if not already set in environment
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
	return scanner.Err()
}
