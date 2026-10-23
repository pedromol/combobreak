package omniroute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the HTTP client communicating with OmniRoute
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient initializes a new OmniRoute client
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	cleanBase := strings.TrimRight(baseURL, "/")

	return &Client{
		BaseURL:    cleanBase,
		APIKey:     apiKey,
		HTTPClient: httpClient,
	}
}

// doRequest performs an authenticated HTTP request
func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	url := fmt.Sprintf("%s%s", c.BaseURL, path)

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
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
		return nil, 0, fmt.Errorf("request to %s %s failed: %w", method, url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
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
