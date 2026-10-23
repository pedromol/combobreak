package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedromol/combobreak/config"
	"github.com/pedromol/combobreak/omniroute"
)

func boolPtr(b bool) *bool {
	return &b
}

func floatPtr(f float64) *float64 {
	return &f
}

func TestRunSync_EndToEnd(t *testing.T) {
	var modelsCalled int32
	var combosListed int32
	var comboPosted int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			atomic.AddInt32(&modelsCalled, 1)
			resp := omniroute.ModelListResponse{
				Object: "list",
				Data: []omniroute.ModelEntry{
					{
						ID: "provider/free-reasoner:free",
						Capabilities: &omniroute.ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
					},
					{
						ID: "provider/free-cost-model",
						Capabilities: &omniroute.ModelCapabilities{
							Tools:    boolPtr(true),
							Thinking: boolPtr(true),
						},
						Pricing: &omniroute.ModelPricing{
							Input:  floatPtr(0),
							Output: floatPtr(0),
						},
					},
					{
						ID: "provider/paid-model",
						Capabilities: &omniroute.ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
						Pricing: &omniroute.ModelPricing{
							Input:  floatPtr(5.0),
							Output: floatPtr(15.0),
						},
					},
					{
						ID: "combobreak", // Should be ignored (prevent self reference)
						Capabilities: &omniroute.ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			atomic.AddInt32(&combosListed, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]omniroute.Combo{})

		case r.Method == http.MethodPost && r.URL.Path == "/api/combos":
			atomic.AddInt32(&comboPosted, 1)
			var c omniroute.Combo
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				t.Fatalf("failed to decode post body: %v", err)
			}
			if c.Name != "combobreak" {
				t.Errorf("expected combo name 'combobreak', got %s", c.Name)
			}
			if len(c.Models) != 2 {
				t.Errorf("expected 2 models in combo, got %d", len(c.Models))
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

		default:
			t.Errorf("unexpected endpoint reached: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cfg := &config.Config{
		BaseURL:   ts.URL,
		APIKey:    "test-secret",
		ComboName: "combobreak",
		Strategy:  "priority",
		Timeout:   5 * time.Second,
		Verbose:   true,
	}

	client := omniroute.NewClient(cfg.BaseURL, cfg.APIKey, ts.Client())
	filter := omniroute.NewModelFilter(cfg.ComboName)

	err := runSync(context.Background(), cfg, client, filter)
	if err != nil {
		t.Fatalf("runSync failed: %v", err)
	}

	if atomic.LoadInt32(&modelsCalled) != 1 {
		t.Errorf("expected /v1/models to be called once, got %d", modelsCalled)
	}
	if atomic.LoadInt32(&combosListed) != 1 {
		t.Errorf("expected /api/combos (GET) to be called once, got %d", combosListed)
	}
	if atomic.LoadInt32(&comboPosted) != 1 {
		t.Errorf("expected /api/combos (POST) to be called once, got %d", comboPosted)
	}
}
