package omniroute

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_GetModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("missing or invalid Authorization header: %s", r.Header.Get("Authorization"))
		}

		resp := ModelListResponse{
			Object: "list",
			Data: []ModelEntry{
				{
					ID: "openrouter/model-a:free",
					Capabilities: &ModelCapabilities{
						ToolCalling: boolPtr(true),
						Reasoning:   boolPtr(true),
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-api-key", ts.Client())
	models, err := client.GetModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 1 || models[0].ID != "openrouter/model-a:free" {
		t.Errorf("unexpected models received: %+v", models)
	}
}

func TestClient_UpsertCombo_CreateNew(t *testing.T) {
	var createdCombo *Combo

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			// Return empty list of combos
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]Combo{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/combos":
			var c Combo
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				t.Fatalf("failed to decode post body: %v", err)
			}
			createdCombo = &c
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-key", ts.Client())
	combo := Combo{
		Name:     "combobreak",
		Strategy: "priority",
		Models: []ComboStep{
			{Model: "openrouter/model-a:free"},
		},
	}

	action, err := client.UpsertCombo(context.Background(), combo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if action != "created" {
		t.Errorf("expected action 'created', got %s", action)
	}
	if createdCombo == nil || createdCombo.Name != "combobreak" {
		t.Errorf("expected combo to be created with name combobreak, got %+v", createdCombo)
	}
}

func TestClient_UpsertCombo_UpdateExisting(t *testing.T) {
	var updatedCombo *Combo

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/combos":
			// Return list containing combobreak
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]Combo{
				{
					ID:       "combo-existing-id",
					Name:     "combobreak",
					Strategy: "priority",
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/combos/combo-existing-id":
			var c Combo
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				t.Fatalf("failed to decode put body: %v", err)
			}
			updatedCombo = &c
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-key", ts.Client())
	combo := Combo{
		Name:     "combobreak",
		Strategy: "priority",
		Models: []ComboStep{
			{Model: "openrouter/model-a:free"},
			{Model: "openrouter/model-b:free"},
		},
	}

	action, err := client.UpsertCombo(context.Background(), combo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if action != "updated" {
		t.Errorf("expected action 'updated', got %s", action)
	}
	if updatedCombo == nil || len(updatedCombo.Models) != 2 {
		t.Errorf("expected combo with 2 models updated, got %+v", updatedCombo)
	}
}

func TestClient_TestBatchProviders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/providers/test-batch" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req ProviderTestBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode req body: %v", err)
		}
		if req.Mode != "all" {
			t.Errorf("expected mode 'all', got %s", req.Mode)
		}

		resp := ProviderTestBatchResponse{
			Results: []ProviderTestResult{
				{Provider: "github", Valid: true},
				{Provider: "ollama-cloud", Valid: true},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	results, err := client.TestBatchProviders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 || results[0].Provider != "github" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestClient_SyncAvailableModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/synced-available-models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("provider") != "ollama-cloud" {
			t.Errorf("expected provider 'ollama-cloud', got %s", r.URL.Query().Get("provider"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"synced": true}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	if err := client.SyncAvailableModels(context.Background(), "ollama-cloud"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_GetProviderModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/provider-models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("provider") != "ollama-cloud" {
			t.Errorf("expected provider 'ollama-cloud', got %s", r.URL.Query().Get("provider"))
		}

		resp := ProviderModelsResponse{
			ModelCompatOverrides: []ModelCompatOverride{
				{ID: "deepseek-v4.1-flash:1017"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	resp, err := client.GetProviderModels(context.Background(), "ollama-cloud")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ModelCompatOverrides) != 1 || resp.ModelCompatOverrides[0].ID != "deepseek-v4.1-flash:1017" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_UnhideProviderModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/provider-models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("provider") != "ollama-cloud" {
			t.Errorf("expected provider 'ollama-cloud', got %s", r.URL.Query().Get("provider"))
		}

		var req PatchProviderModelsBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode req body: %v", err)
		}
		if req.IsHidden || req.Modality != "chat" || len(req.ModelIDs) != 1 || req.ModelIDs[0] != "gemma4:31b" {
			t.Errorf("unexpected req body: %+v", req)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	if err := client.UnhideProviderModels(context.Background(), "ollama-cloud", []string{"gemma4:31b"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_TestModel_SuccessAndFunctionalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/models/test" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req ModelTestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode req body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		if req.ModelID == "ok-model" {
			_ = json.NewEncoder(w).Encode(ModelTestResponse{
				Status:       "ok",
				LatencyMs:    123,
				ResponseText: "pong",
			})
		} else {
			w.WriteHeader(http.StatusPaymentRequired) // 402
			_ = json.NewEncoder(w).Encode(ModelTestResponse{
				Status:     "error",
				LatencyMs:  456,
				Error:      "quota exhausted",
				StatusCode: 402,
			})
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())

	// Test success
	okResp, err := client.TestModel(context.Background(), "github", "ok-model")
	if err != nil {
		t.Fatalf("unexpected error for ok-model: %v", err)
	}
	if okResp.Status != "ok" || okResp.ResponseText != "pong" {
		t.Errorf("unexpected okResp: %+v", okResp)
	}

	// Test functional error (402 quota error)
	errResp, err := client.TestModel(context.Background(), "github", "exhausted-model")
	if err != nil {
		t.Fatalf("unexpected transport error for exhausted-model: %v", err)
	}
	if errResp.Status != "error" || errResp.StatusCode != 402 {
		t.Errorf("unexpected errResp: %+v", errResp)
	}
}

func TestClient_HideProviderModel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/provider-models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("provider") != "ollama-cloud" {
			t.Errorf("expected provider 'ollama-cloud', got %s", r.URL.Query().Get("provider"))
		}
		if r.URL.Query().Get("modelId") != "mistral-large-3:675b" {
			t.Errorf("expected modelId 'mistral-large-3:675b', got %s", r.URL.Query().Get("modelId"))
		}

		var req PatchProviderModelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode req body: %v", err)
		}
		if !req.IsHidden || req.Modality != "chat" {
			t.Errorf("unexpected patch body: %+v", req)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	if err := client.HideProviderModel(context.Background(), "ollama-cloud", "mistral-large-3:675b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_RetryWithBackoff_Transient500(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "key", ts.Client())
	client.RetryBaseDelay = 5 * time.Millisecond // fast backoff in test

	respBody, status, err := client.doRequestWithRetry(context.Background(), http.MethodGet, "/test", nil, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK || string(respBody) != `{"ok": true}` {
		t.Errorf("unexpected response: status=%d, body=%s", status, string(respBody))
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestNewClient_DefaultHTTPClient(t *testing.T) {
	client := NewClient("http://localhost:20128", "test-key", nil)
	if client.HTTPClient == nil {
		t.Fatal("expected non-nil HTTPClient")
	}
	if client.HTTPClient.Timeout != 60*time.Second {
		t.Errorf("expected default timeout 60s, got %v", client.HTTPClient.Timeout)
	}
}

