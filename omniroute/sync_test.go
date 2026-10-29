package omniroute

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSyncCoordinator_HappyPath(t *testing.T) {
	var (
		batchCalled     int32
		syncedCalled    int32
		provModelsCalls int32
		unhideCalls     int32
		modelTests      int32
		hideCalls       int32
		comboPosts      int32
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			resp := ModelListResponse{
				Data: []ModelEntry{
					{
						ID: "good-model",
						Capabilities: &ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
					},
					{
						ID: "bad-model",
						Capabilities: &ModelCapabilities{
							ToolCalling: boolPtr(true),
							Reasoning:   boolPtr(true),
						},
					},
					{
						ID: "no-tools-model",
						Capabilities: &ModelCapabilities{
							ToolCalling: boolPtr(false),
							Reasoning:   boolPtr(true),
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPost && r.URL.Path == "/api/providers/test-batch":
			atomic.AddInt32(&batchCalled, 1)
			errStr := "auth error"
			resp := ProviderTestBatchResponse{
				Results: []ProviderTestResult{
					{Provider: "prov-ok", Valid: true},
					{Provider: "prov-fail", Valid: false, Error: &errStr},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodGet && r.URL.Path == "/api/synced-available-models":
			atomic.AddInt32(&syncedCalled, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"synced": true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/provider-models":
			atomic.AddInt32(&provModelsCalls, 1)
			resp := ProviderModelsResponse{
				ModelCompatOverrides: []ModelCompatOverride{
					{ID: "good-model"},
					{ID: "bad-model"},
					{ID: "no-tools-model"}, // should be filtered out by capabilities
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models" && r.URL.Query().Get("modelId") == "":
			atomic.AddInt32(&unhideCalls, 1)
			var req PatchProviderModelsBatchRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.ModelIDs) != 2 {
				t.Errorf("expected 2 candidate models unhidden, got %d", len(req.ModelIDs))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true}`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/models/test":
			atomic.AddInt32(&modelTests, 1)
			var req ModelTestRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.ModelID == "good-model" {
				_ = json.NewEncoder(w).Encode(ModelTestResponse{
					Status:       "ok",
					LatencyMs:    150,
					ResponseText: "pong",
				})
			} else {
				w.WriteHeader(http.StatusPaymentRequired)
				_ = json.NewEncoder(w).Encode(ModelTestResponse{
					Status:     "error",
					Error:      "quota exhausted",
					StatusCode: 402,
				})
			}

		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models" && r.URL.Query().Get("modelId") == "bad-model":
			atomic.AddInt32(&hideCalls, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			_ = json.NewEncoder(w).Encode([]Combo{})

		case r.Method == http.MethodPost && r.URL.Path == "/api/combos":
			atomic.AddInt32(&comboPosts, 1)
			var c Combo
			_ = json.NewDecoder(r.Body).Decode(&c)
			if len(c.Models) != 1 || c.Models[0].Model != "good-model" || c.Models[0].ProviderID != "prov-ok" {
				t.Errorf("unexpected combo models: %+v", c.Models)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"success": true}`))

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-key", ts.Client())
	client.RetryBaseDelay = 5 * time.Millisecond
	filter := NewModelFilter("combobreak")
	coordinator := NewSyncCoordinator(client, filter)

	report, err := coordinator.Sync(context.Background(), "combobreak", "context-optimized", false, true)
	if err != nil {
		t.Fatalf("unexpected sync error: %v", err)
	}

	if report.TotalProvidersChecked != 2 {
		t.Errorf("expected 2 providers checked, got %d", report.TotalProvidersChecked)
	}
	if report.ProvidersPassed != 1 {
		t.Errorf("expected 1 provider passed, got %d", report.ProvidersPassed)
	}
	if report.TotalModelsTested != 2 {
		t.Errorf("expected 2 models tested, got %d", report.TotalModelsTested)
	}
	if len(report.ModelsApproved) != 1 || report.ModelsApproved[0].Model != "good-model" {
		t.Errorf("unexpected approved models: %+v", report.ModelsApproved)
	}
	if len(report.ModelsFailed) != 1 || report.ModelsFailed[0] != "prov-ok/bad-model" {
		t.Errorf("unexpected failed models: %+v", report.ModelsFailed)
	}
	if report.Action != "created" {
		t.Errorf("expected action 'created', got %s", report.Action)
	}

	if atomic.LoadInt32(&batchCalled) != 1 {
		t.Errorf("expected batchCalled 1, got %d", batchCalled)
	}
	if atomic.LoadInt32(&syncedCalled) != 1 {
		t.Errorf("expected syncedCalled 1, got %d", syncedCalled)
	}
	if atomic.LoadInt32(&provModelsCalls) != 1 {
		t.Errorf("expected provModelsCalls 1, got %d", provModelsCalls)
	}
	if atomic.LoadInt32(&unhideCalls) != 1 {
		t.Errorf("expected unhideCalls 1, got %d", unhideCalls)
	}
	if atomic.LoadInt32(&modelTests) != 2 {
		t.Errorf("expected modelTests 2, got %d", modelTests)
	}
	if atomic.LoadInt32(&hideCalls) != 1 {
		t.Errorf("expected hideCalls 1, got %d", hideCalls)
	}
	if atomic.LoadInt32(&comboPosts) != 1 {
		t.Errorf("expected comboPosts 1, got %d", comboPosts)
	}
}

func TestSyncCoordinator_DryRun(t *testing.T) {
	var comboPosted int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(ModelListResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/providers/test-batch":
			_ = json.NewEncoder(w).Encode(ProviderTestBatchResponse{
				Results: []ProviderTestResult{{Provider: "prov", Valid: true}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/synced-available-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/provider-models":
			_ = json.NewEncoder(w).Encode(ProviderModelsResponse{
				Models: []ModelEntry{{ID: "m1"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/test":
			_ = json.NewEncoder(w).Encode(ModelTestResponse{Status: "ok"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/combos":
			atomic.AddInt32(&comboPosted, 1)
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	client.RetryBaseDelay = 5 * time.Millisecond
	filter := NewModelFilter("combobreak")
	coordinator := NewSyncCoordinator(client, filter)

	report, err := coordinator.Sync(context.Background(), "combobreak", "priority", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Action != "dry-run" {
		t.Errorf("expected action 'dry-run', got %s", report.Action)
	}
	if atomic.LoadInt32(&comboPosted) != 0 {
		t.Errorf("expected no combo POST in dry-run mode, got %d", comboPosted)
	}
}

func TestSyncCoordinator_AllModelsFailed_PreservesCombo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(ModelListResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/providers/test-batch":
			_ = json.NewEncoder(w).Encode(ProviderTestBatchResponse{
				Results: []ProviderTestResult{{Provider: "prov", Valid: true}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/synced-available-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/provider-models":
			_ = json.NewEncoder(w).Encode(ProviderModelsResponse{
				Models: []ModelEntry{{ID: "m1"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/test":
			_ = json.NewEncoder(w).Encode(ModelTestResponse{Status: "error", Error: "quota exceeded"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/combos":
			t.Errorf("combos endpoint should not be invoked when all models fail")
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	client.RetryBaseDelay = 5 * time.Millisecond
	filter := NewModelFilter("combobreak")
	coordinator := NewSyncCoordinator(client, filter)

	report, err := coordinator.Sync(context.Background(), "combobreak", "priority", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Action != "skipped" {
		t.Errorf("expected action 'skipped', got %s", report.Action)
	}
	if len(report.ModelsApproved) != 0 {
		t.Errorf("expected 0 approved models, got %d", len(report.ModelsApproved))
	}
}

func TestSyncCoordinator_IncrementalUpdatesPerModel(t *testing.T) {
	var (
		comboCreated int32
		comboUpdated int32
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(ModelListResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/providers/test-batch":
			_ = json.NewEncoder(w).Encode(ProviderTestBatchResponse{
				Results: []ProviderTestResult{{Provider: "prov-a", Valid: true}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/synced-available-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/provider-models":
			_ = json.NewEncoder(w).Encode(ProviderModelsResponse{
				Models: []ModelEntry{{ID: "model-1"}, {ID: "model-2"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/test":
			_ = json.NewEncoder(w).Encode(ModelTestResponse{Status: "ok"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			_ = json.NewEncoder(w).Encode([]Combo{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/combos":
			atomic.AddInt32(&comboCreated, 1)
			var c Combo
			_ = json.NewDecoder(r.Body).Decode(&c)
			if len(c.Models) != 1 || c.Models[0].Model != "model-1" {
				t.Errorf("expected combo created with 1st model 'model-1', got %+v", c.Models)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "combobreak"})
		case r.Method == http.MethodPut && r.URL.Path == "/api/combos/combobreak":
			atomic.AddInt32(&comboUpdated, 1)
			var c Combo
			_ = json.NewDecoder(r.Body).Decode(&c)
			if len(c.Models) != 2 || c.Models[1].Model != "model-2" {
				t.Errorf("expected combo updated with 2 models including 'model-2', got %+v", c.Models)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-key", ts.Client())
	client.RetryBaseDelay = 5 * time.Millisecond
	filter := NewModelFilter("combobreak")
	coordinator := NewSyncCoordinator(client, filter)

	report, err := coordinator.Sync(context.Background(), "combobreak", "priority", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Action != "updated" {
		t.Errorf("expected action 'updated', got %q", report.Action)
	}
	if len(report.ModelsApproved) != 2 {
		t.Errorf("expected 2 approved models, got %d", len(report.ModelsApproved))
	}
	if atomic.LoadInt32(&comboCreated) != 1 {
		t.Errorf("expected combo to be created upon 1st discovery, got %d calls", comboCreated)
	}
	if atomic.LoadInt32(&comboUpdated) != 1 {
		t.Errorf("expected combo to be updated upon 2nd discovery, got %d calls", comboUpdated)
	}
}

func TestSyncCoordinator_RemovesFailedExistingModel(t *testing.T) {
	var comboUpdated int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(ModelListResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/providers/test-batch":
			_ = json.NewEncoder(w).Encode(ProviderTestBatchResponse{
				Results: []ProviderTestResult{{Provider: "prov-a", Valid: true}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/synced-available-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/provider-models":
			_ = json.NewEncoder(w).Encode(ProviderModelsResponse{
				Models: []ModelEntry{{ID: "broken-model"}, {ID: "good-model"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/provider-models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/test":
			var req ModelTestRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.ModelID == "broken-model" {
				_ = json.NewEncoder(w).Encode(ModelTestResponse{Status: "error", Error: "quota depleted"})
			} else {
				_ = json.NewEncoder(w).Encode(ModelTestResponse{Status: "ok"})
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			// Existing combo has both models
			_ = json.NewEncoder(w).Encode([]Combo{
				{
					ID:   "combobreak",
					Name: "combobreak",
					Models: []ComboStep{
						{Model: "broken-model", ProviderID: "prov-a"},
						{Model: "preserved-model", ProviderID: "prov-other"},
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/combos/combobreak":
			atomic.AddInt32(&comboUpdated, 1)
			var c Combo
			_ = json.NewDecoder(r.Body).Decode(&c)
			// On 1st update: broken-model is removed, preserved-model remains
			// On 2nd update: good-model is added
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-key", ts.Client())
	client.RetryBaseDelay = 5 * time.Millisecond
	filter := NewModelFilter("combobreak")
	coordinator := NewSyncCoordinator(client, filter)

	report, err := coordinator.Sync(context.Background(), "combobreak", "priority", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Action != "updated" {
		t.Errorf("expected action 'updated', got %q", report.Action)
	}
	if len(report.ModelsFailed) != 1 || report.ModelsFailed[0] != "prov-a/broken-model" {
		t.Errorf("unexpected failed models: %+v", report.ModelsFailed)
	}
	// Models remaining: preserved-model + good-model = 2
	if len(report.ModelsApproved) != 2 {
		t.Errorf("expected 2 approved models in combo, got %d: %+v", len(report.ModelsApproved), report.ModelsApproved)
	}
	// comboUpdated should have been called twice: 1 for removal of broken-model, 1 for addition of good-model
	if atomic.LoadInt32(&comboUpdated) != 2 {
		t.Errorf("expected 2 incremental PUT calls to combo, got %d", comboUpdated)
	}
}
