package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadFromEnvAndFlags(t *testing.T) {
	_ = os.Setenv("OMNIROUTE_BASE_URL", "http://127.0.0.1:8080/")
	_ = os.Setenv("OMNIROUTE_API_KEY", "env-api-key")
	defer func() {
		_ = os.Unsetenv("OMNIROUTE_BASE_URL")
		_ = os.Unsetenv("OMNIROUTE_API_KEY")
	}()

	cfg, err := LoadFromEnvAndFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("expected trimmed base url http://127.0.0.1:8080, got %s", cfg.BaseURL)
	}
	if cfg.APIKey != "env-api-key" {
		t.Errorf("expected API key env-api-key, got %s", cfg.APIKey)
	}
	if cfg.ComboName != "combobreak" {
		t.Errorf("expected default combo name combobreak, got %s", cfg.ComboName)
	}
	if cfg.Strategy != "context-optimized" {
		t.Errorf("expected default strategy context-optimized, got %s", cfg.Strategy)
	}
	if cfg.Timeout != 3*time.Second {
		t.Errorf("expected default timeout 3s, got %v", cfg.Timeout)
	}
}

func TestLoadFromEnvAndFlags_FlagOverride(t *testing.T) {
	args := []string{
		"-base-url", "https://api.omniroute.local",
		"-api-key", "flag-key",
		"-combo-name", "custom-combo",
		"-dry-run",
		"-verbose",
		"-interval", "15m",
	}

	cfg, err := LoadFromEnvAndFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BaseURL != "https://api.omniroute.local" {
		t.Errorf("expected base URL override, got %s", cfg.BaseURL)
	}
	if cfg.APIKey != "flag-key" {
		t.Errorf("expected API key override, got %s", cfg.APIKey)
	}
	if cfg.ComboName != "custom-combo" {
		t.Errorf("expected combo name override, got %s", cfg.ComboName)
	}
	if !cfg.DryRun {
		t.Errorf("expected DryRun=true")
	}
	if !cfg.Verbose {
		t.Errorf("expected Verbose=true")
	}
	if cfg.Interval != 15*time.Minute {
		t.Errorf("expected Interval 15m, got %v", cfg.Interval)
	}
}
