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

func TestRunSync_EndToEnd(t *testing.T) {
	var (
		catalogCalled int32
		batchCalled   int32
		syncCalled    int32
		modelsCalled  int32
		unhideCalled  int32
		testCalled    int32
		hideCalled    int32
		combosListed  int32
		comboPosted   int32
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			atomic.AddInt32(&catalogCalled, 1)
			resp := omniroute.ModelListResponse{
				Object: "list",
				Data: []omniroute.ModelEntry{
					{
						ID: "github/auto",
						Capabilities: &omniroute.ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
					},
					{
						ID: "mistral-large-3:675b",
						Capabilities: &omniroute.ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPost && r.URL.Path == "/api/providers/test-batch":
			atomic.AddInt32(&batchCalled, 1)
			errMsg := "Invalid API key"
			resp := omniroute.ProviderTestBatchResponse{
				Results: []omniroute.ProviderTestResult{
					{
						Provider: "github",
						Valid:    true,
						Error:    nil,
					},
					{
						Provider: "uncloseai",
						Valid:    false,
						Error:    &errMsg,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodGet && r.URL.Path == "/api/synced-available-models":
			atomic.AddInt32(&syncCalled, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"synced":true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/provider-models":
			atomic.AddInt32(&modelsCalled, 1)
			resp := omniroute.ProviderModelsResponse{
				ModelCompatOverrides: []omniroute.ModelCompatOverride{
					{ID: "gh/auto"},
					{ID: "mistral-large-3:675b"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models" && r.URL.Query().Get("modelId") == "":
			atomic.AddInt32(&unhideCalled, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"updated":2}`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/models/test":
			atomic.AddInt32(&testCalled, 1)
			var req omniroute.ModelTestRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.ModelID == "gh/auto" {
				_ = json.NewEncoder(w).Encode(omniroute.ModelTestResponse{
					Status:       "ok",
					LatencyMs:    2470,
					ResponseText: "pongpongpongpong",
				})
			} else {
				w.WriteHeader(http.StatusPaymentRequired)
				_ = json.NewEncoder(w).Encode(omniroute.ModelTestResponse{
					Status:     "error",
					LatencyMs:  636,
					Error:      "[402]: this model is not included in your free usage",
					StatusCode: 402,
				})
			}

		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models" && r.URL.Query().Get("modelId") == "mistral-large-3:675b":
			atomic.AddInt32(&hideCalled, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			atomic.AddInt32(&combosListed, 1)
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
			if len(c.Models) != 1 {
				t.Errorf("expected exactly 1 approved model in combo, got %d", len(c.Models))
			}
			if c.Models[0].Model != "gh/auto" || c.Models[0].ProviderID != "github" {
				t.Errorf("unexpected model in combo: %+v", c.Models[0])
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
	client.RetryBaseDelay = 5 * time.Millisecond
	filter := omniroute.NewModelFilter(cfg.ComboName)
	coordinator := omniroute.NewSyncCoordinator(client, filter)

	err := runSync(context.Background(), cfg, coordinator)
	if err != nil {
		t.Fatalf("runSync failed: %v", err)
	}

	if atomic.LoadInt32(&catalogCalled) != 1 {
		t.Errorf("expected /v1/models called 1, got %d", catalogCalled)
	}
	if atomic.LoadInt32(&batchCalled) != 1 {
		t.Errorf("expected test-batch called 1, got %d", batchCalled)
	}
	if atomic.LoadInt32(&syncCalled) != 1 {
		t.Errorf("expected synced-available-models called 1, got %d", syncCalled)
	}
	if atomic.LoadInt32(&modelsCalled) != 1 {
		t.Errorf("expected provider-models called 1, got %d", modelsCalled)
	}
	if atomic.LoadInt32(&unhideCalled) != 1 {
		t.Errorf("expected unhide called 1, got %d", unhideCalled)
	}
	if atomic.LoadInt32(&testCalled) != 2 {
		t.Errorf("expected 2 model tests called, got %d", testCalled)
	}
	if atomic.LoadInt32(&hideCalled) != 1 {
		t.Errorf("expected 1 model hide called, got %d", hideCalled)
	}
	if atomic.LoadInt32(&combosListed) != 1 {
		t.Errorf("expected /api/combos listed 1, got %d", combosListed)
	}
	if atomic.LoadInt32(&comboPosted) != 1 {
		t.Errorf("expected /api/combos posted 1, got %d", comboPosted)
	}
}
