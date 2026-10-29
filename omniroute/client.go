package omniroute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the HTTP client communicating with OmniRoute
type Client struct {
	BaseURL        string
	APIKey         string
	HTTPClient     *http.Client
	RetryBaseDelay time.Duration
}

// NewClient initializes a new OmniRoute client
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 3 * time.Second,
		}
	}

	cleanBase := strings.TrimRight(baseURL, "/")

	return &Client{
		BaseURL:        cleanBase,
		APIKey:         apiKey,
		HTTPClient:     httpClient,
		RetryBaseDelay: 1 * time.Second,
	}
}

// doRequest performs an authenticated HTTP request
func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	reqURL := fmt.Sprintf("%s%s", c.BaseURL, path)

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request to %s %s failed: %w", method, reqURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// doRequestWithRetry executes doRequest with exponential backoff on transient errors
func (c *Client) doRequestWithRetry(ctx context.Context, method, path string, body any, maxRetries int) ([]byte, int, error) {
	baseDelay := c.RetryBaseDelay
	if baseDelay <= 0 {
		baseDelay = 1 * time.Second
	}

	var lastBody []byte
	var lastStatus int
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * (1 << (attempt - 1))
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
			}
		}

		lastBody, lastStatus, lastErr = c.doRequest(ctx, method, path, body)
		if lastErr != nil {
			// Network or connection error: retry
			continue
		}

		// Retry only on rate limits (429) or transient server errors (5xx)
		if lastStatus == http.StatusTooManyRequests || (lastStatus >= 500 && lastStatus <= 599) {
			continue
		}

		// Any other status code (2xx, 3xx, 4xx non-429) returns immediately
		return lastBody, lastStatus, nil
	}

	if lastErr != nil {
		return lastBody, lastStatus, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
	}
	return lastBody, lastStatus, fmt.Errorf("request returned HTTP %d after %d retries: %s", lastStatus, maxRetries, string(lastBody))
}

// TestBatchProviders queries POST /api/providers/test-batch to test all configured providers
func (c *Client) TestBatchProviders(ctx context.Context) ([]ProviderTestResult, error) {
	reqBody := ProviderTestBatchRequest{
		Mode:       "all",
		ProviderID: nil,
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPost, "/api/providers/test-batch", reqBody, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to test providers batch: %w", err)
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("POST /api/providers/test-batch returned HTTP %d: %s", statusCode, string(respBody))
	}

	var batchResp ProviderTestBatchResponse
	if err := json.Unmarshal(respBody, &batchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal test-batch response: %w", err)
	}

	return batchResp.Results, nil
}

// SyncAvailableModels queries GET /api/synced-available-models?provider=<provider>
func (c *Client) SyncAvailableModels(ctx context.Context, provider string) error {
	path := fmt.Sprintf("/api/synced-available-models?provider=%s", url.QueryEscape(provider))
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil, 3)
	if err != nil {
		return fmt.Errorf("failed to sync available models for provider %q: %w", provider, err)
	}

	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("GET %s returned HTTP %d: %s", path, statusCode, string(respBody))
	}

	return nil
}

// GetProviderModels queries GET /api/provider-models?provider=<provider>
func (c *Client) GetProviderModels(ctx context.Context, provider string) (*ProviderModelsResponse, error) {
	path := fmt.Sprintf("/api/provider-models?provider=%s", url.QueryEscape(provider))
	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodGet, path, nil, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider models for %q: %w", provider, err)
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned HTTP %d: %s", path, statusCode, string(respBody))
	}

	var modelsResp ProviderModelsResponse
	if err := json.Unmarshal(respBody, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider models response for %q: %w", provider, err)
	}

	return &modelsResp, nil
}

// UnhideProviderModels sends PATCH /api/provider-models?provider=<provider> to unhide candidate chat models
func (c *Client) UnhideProviderModels(ctx context.Context, provider string, modelIDs []string) error {
	path := fmt.Sprintf("/api/provider-models?provider=%s", url.QueryEscape(provider))
	reqBody := PatchProviderModelsBatchRequest{
		IsHidden: false,
		ModelIDs: modelIDs,
		Modality: "chat",
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPatch, path, reqBody, 3)
	if err != nil {
		return fmt.Errorf("failed to unhide models for provider %q: %w", provider, err)
	}

	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("PATCH %s returned HTTP %d: %s", path, statusCode, string(respBody))
	}

	return nil
}

// TestModel performs an active test on a specific model via POST /api/models/test
func (c *Client) TestModel(ctx context.Context, providerID, modelID string) (*ModelTestResponse, error) {
	reqBody := ModelTestRequest{
		ProviderID: providerID,
		ModelID:    modelID,
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPost, "/api/models/test", reqBody, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to test model %s/%s: %w", providerID, modelID, err)
	}

	// Models may fail functional verification with HTTP 4xx (e.g. 402 Payment Required).
	// If response is valid JSON with status field, return it as functional test result.
	var testResp ModelTestResponse
	if unmarshalErr := json.Unmarshal(respBody, &testResp); unmarshalErr == nil && testResp.Status != "" {
		if testResp.StatusCode == 0 {
			testResp.StatusCode = statusCode
		}
		return &testResp, nil
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("POST /api/models/test returned HTTP %d: %s", statusCode, string(respBody))
	}

	return &testResp, nil
}

// HideProviderModel sends PATCH /api/provider-models?provider=<provider>&modelId=<modelId> to hide a failing model
func (c *Client) HideProviderModel(ctx context.Context, providerID, modelID string) error {
	path := fmt.Sprintf("/api/provider-models?provider=%s&modelId=%s", url.QueryEscape(providerID), url.QueryEscape(modelID))
	reqBody := PatchProviderModelRequest{
		IsHidden: true,
		Modality: "chat",
	}

	respBody, statusCode, err := c.doRequestWithRetry(ctx, http.MethodPatch, path, reqBody, 3)
	if err != nil {
		return fmt.Errorf("failed to hide model %s/%s: %w", providerID, modelID, err)
	}

	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("PATCH %s returned HTTP %d: %s", path, statusCode, string(respBody))
	}

	return nil
}


// GetModels retrieves the list of supported models from GET /v1/models
func (c *Client) GetModels(ctx context.Context) ([]ModelEntry, error) {
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("GET /v1/models returned HTTP %d: %s", statusCode, string(respBody))
	}

	models, err := UnmarshalModelEntries(respBody)
	if err != nil {
		return nil, fmt.Errorf("failed to parse models from /v1/models: %w", err)
	}

	return models, nil
}

// ListCombos retrieves all configured combos from GET /api/combos
func (c *Client) ListCombos(ctx context.Context) ([]Combo, error) {
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/combos", nil)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("GET /api/combos returned HTTP %d: %s", statusCode, string(respBody))
	}

	combos, err := UnmarshalCombos(respBody)
	if err != nil {
		return nil, fmt.Errorf("failed to parse combos from /api/combos: %w", err)
	}

	return combos, nil
}

// CreateCombo creates a new combo via POST /api/combos
func (c *Client) CreateCombo(ctx context.Context, combo Combo) error {
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, "/api/combos", combo)
	if err != nil {
		return err
	}

	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("POST /api/combos returned HTTP %d: %s", statusCode, string(respBody))
	}

	return nil
}

// UpdateCombo updates an existing combo via PUT /api/combos/{id}
func (c *Client) UpdateCombo(ctx context.Context, id string, combo Combo) error {
	path := fmt.Sprintf("/api/combos/%s", id)
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPut, path, combo)
	if err != nil {
		return err
	}

	// Some implementations support PATCH if PUT returns 405 Method Not Allowed
	if statusCode == http.StatusMethodNotAllowed {
		respBody, statusCode, err = c.doRequest(ctx, http.MethodPatch, path, combo)
		if err != nil {
			return err
		}
	}

	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("PUT %s returned HTTP %d: %s", path, statusCode, string(respBody))
	}

	return nil
}

// UpsertCombo checks if the combo exists and calls Update or Create accordingly
func (c *Client) UpsertCombo(ctx context.Context, combo Combo) (action string, err error) {
	existingCombos, listErr := c.ListCombos(ctx)

	var existing *Combo
	if listErr == nil {
		for _, ex := range existingCombos {
			if strings.EqualFold(ex.Name, combo.Name) || (combo.ID != "" && strings.EqualFold(ex.ID, combo.ID)) {
				existing = &ex
				break
			}
		}
	}

	if existing != nil {
		// Target ID to update
		targetID := existing.ID
		if targetID == "" {
			targetID = existing.Name
		}
		if combo.ID == "" {
			combo.ID = targetID
		}

		err = c.UpdateCombo(ctx, targetID, combo)
		if err == nil {
			return "updated", nil
		}

		// Fallback to POST if PUT returned 404 Not Found
		if strings.Contains(err.Error(), "404") {
			if postErr := c.CreateCombo(ctx, combo); postErr == nil {
				return "created", nil
			}
		}

		return "", fmt.Errorf("failed to update combo %q (id: %s): %w", combo.Name, targetID, err)
	}

	// Combo does not exist yet (or ListCombos was not accessible, e.g. 404)
	err = c.CreateCombo(ctx, combo)
	if err == nil {
		return "created", nil
	}

	// If Create returned 409 Conflict, try update by name
	if strings.Contains(err.Error(), "409") || strings.Contains(strings.ToLower(err.Error()), "already exists") {
		targetID := combo.ID
		if targetID == "" {
			targetID = combo.Name
		}
		if updateErr := c.UpdateCombo(ctx, targetID, combo); updateErr == nil {
			return "updated", nil
		}
	}

	return "", fmt.Errorf("failed to create combo %q: %w", combo.Name, err)
}
