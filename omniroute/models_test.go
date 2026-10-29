package omniroute

import (
	"encoding/json"
	"testing"
)

func TestProviderTestBatchResponse_Unmarshal(t *testing.T) {
	rawJSON := `{
		"mode": "all",
		"providerId": null,
		"results": [
			{
				"provider": "github",
				"connectionId": "73c6e925-ed79-4068-b604-dbcf391b6dec",
				"connectionName": "github",
				"authType": "oauth",
				"valid": true,
				"latencyMs": 350,
				"error": null,
				"diagnosis": {"type": "ok", "source": "upstream", "message": null, "code": null},
				"statusCode": null,
				"testedAt": "2026-10-29T13:44:04.582Z"
			},
			{
				"provider": "uncloseai",
				"connectionId": "99d07dab-088f-44e9-b372-fe6d469404f0",
				"connectionName": "main",
				"authType": "apikey",
				"valid": false,
				"latencyMs": 663,
				"error": "Invalid API key",
				"diagnosis": {"type": "upstream_auth_error", "source": "upstream", "message": "Invalid API key", "code": "auth_failed"},
				"statusCode": null,
				"testedAt": "2026-10-29T13:44:04.894Z"
			}
		],
		"testedAt": "2026-10-29T13:44:21.547Z",
		"summary": {"total": 2, "passed": 1, "failed": 1}
	}`

	var resp ProviderTestBatchResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		t.Fatalf("failed to unmarshal ProviderTestBatchResponse: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].Provider != "github" || !resp.Results[0].Valid || resp.Results[0].Error != nil {
		t.Errorf("unexpected first provider result: %+v", resp.Results[0])
	}
	if resp.Results[1].Provider != "uncloseai" || resp.Results[1].Valid || resp.Results[1].Error == nil || *resp.Results[1].Error != "Invalid API key" {
		t.Errorf("unexpected second provider result: %+v", resp.Results[1])
	}
	if resp.Summary == nil || resp.Summary.Total != 2 || resp.Summary.Passed != 1 || resp.Summary.Failed != 1 {
		t.Errorf("unexpected summary: %+v", resp.Summary)
	}
}

func TestProviderModelsResponse_Unmarshal(t *testing.T) {
	rawJSON := `{
		"models": [
			{"id": "gpt-4o", "name": "GPT-4o"}
		],
		"modelCompatOverrides": [
			{"id": "deepseek-v4-flash:0731", "isHidden": true},
			{"id": "deepseek-v4-pro", "isHidden": false, "hiddenModalities": {"chat": false}}
		],
		"hiddenModelsByProvider": {
			"ollama-cloud": ["deepseek-v4-flash:0731"]
		}
	}`

	var resp ProviderModelsResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		t.Fatalf("failed to unmarshal ProviderModelsResponse: %v", err)
	}

	if len(resp.Models) != 1 || resp.Models[0].ID != "gpt-4o" {
		t.Errorf("unexpected models: %+v", resp.Models)
	}
	if len(resp.ModelCompatOverrides) != 2 {
		t.Errorf("unexpected overrides: %+v", resp.ModelCompatOverrides)
	}
	if resp.ModelCompatOverrides[0].IsHidden == nil || !*resp.ModelCompatOverrides[0].IsHidden {
		t.Errorf("expected isHidden=true for first override")
	}
	if resp.ModelCompatOverrides[1].IsHidden == nil || *resp.ModelCompatOverrides[1].IsHidden {
		t.Errorf("expected isHidden=false for second override")
	}
	if len(resp.HiddenModelsByProvider["ollama-cloud"]) != 1 {
		t.Errorf("expected hidden models for ollama-cloud: %+v", resp.HiddenModelsByProvider)
	}
}

func TestModelTestResponse_Unmarshal(t *testing.T) {
	successJSON := `{"status":"ok","latencyMs":2470,"responseText":"pongpongpongpong"}`
	var okResp ModelTestResponse
	if err := json.Unmarshal([]byte(successJSON), &okResp); err != nil {
		t.Fatalf("failed to unmarshal ok response: %v", err)
	}
	if okResp.Status != "ok" || okResp.LatencyMs != 2470 || okResp.ResponseText != "pongpongpongpong" {
		t.Errorf("unexpected ok response: %+v", okResp)
	}

	errorJSON := `{"status":"error","latencyMs":636,"error":"[402]: this model is not included in your free usage","statusCode":402}`
	var errResp ModelTestResponse
	if err := json.Unmarshal([]byte(errorJSON), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}
	if errResp.Status != "error" || errResp.StatusCode != 402 || errResp.Error == "" {
		t.Errorf("unexpected error response: %+v", errResp)
	}
}
