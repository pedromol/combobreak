package omniroute

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
