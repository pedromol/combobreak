package omniroute

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ModelListResponse represents the response from GET /v1/models
type ModelListResponse struct {
	Object string       `json:"object,omitempty"`
	Data   []ModelEntry `json:"data"`
}

// ModelEntry represents a single model in /v1/models
type ModelEntry struct {
	ID              string             `json:"id"`
	Object          string             `json:"object,omitempty"`
	Created         int64              `json:"created,omitempty"`
	OwnedBy         string             `json:"owned_by,omitempty"`
	Root            string             `json:"root,omitempty"`
	Name            string             `json:"name,omitempty"`
	Description     string             `json:"description,omitempty"`
	ContextLength   int64              `json:"context_length,omitempty"`
	MaxOutputTokens int64              `json:"max_output_tokens,omitempty"`
	Capabilities    *ModelCapabilities `json:"capabilities,omitempty"`
	Pricing         *ModelPricing      `json:"pricing,omitempty"`
}

// ModelCapabilities captures model features
type ModelCapabilities struct {
	ToolCalling      *bool `json:"tool_calling,omitempty"`
	Tools            *bool `json:"tools,omitempty"`
	FunctionCalling  *bool `json:"function_calling,omitempty"`
	Reasoning        *bool `json:"reasoning,omitempty"`
	Thinking         *bool `json:"thinking,omitempty"`
	SupportsThinking *bool `json:"supportsThinking,omitempty"`
	Vision           *bool `json:"vision,omitempty"`
	// EffortTiers can be an array of string tiers or a boolean
	EffortTiers any `json:"effort_tiers,omitempty"`
}

// ModelPricing represents pricing information
type ModelPricing struct {
	Input     *float64 `json:"input,omitempty"`
	Output    *float64 `json:"output,omitempty"`
	Cached    *float64 `json:"cached,omitempty"`
	Reasoning *float64 `json:"reasoning,omitempty"`
	IsFree    *bool    `json:"is_free,omitempty"`
}

// UnmarshalJSON custom unmarshaler for ModelPricing to handle numeric strings or numbers
func (p *ModelPricing) UnmarshalJSON(data []byte) error {
	type rawPricing struct {
		Input     any   `json:"input,omitempty"`
		Output    any   `json:"output,omitempty"`
		Cached    any   `json:"cached,omitempty"`
		Reasoning any   `json:"reasoning,omitempty"`
		IsFree    *bool `json:"is_free,omitempty"`
	}

	var raw rawPricing
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.IsFree = raw.IsFree
	if v, ok := toFloat64(raw.Input); ok {
		p.Input = &v
	}
	if v, ok := toFloat64(raw.Output); ok {
		p.Output = &v
	}
	if v, ok := toFloat64(raw.Cached); ok {
		p.Cached = &v
	}
	if v, ok := toFloat64(raw.Reasoning); ok {
		p.Reasoning = &v
	}

	return nil
}

func toFloat64(val any) (float64, bool) {
	if val == nil {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// ComboStep represents an item in the combo models array
type ComboStep struct {
	Kind                         string   `json:"kind,omitempty"`
	Model                        string   `json:"model"`
	ProviderID                   string   `json:"providerId,omitempty"`
	ConnectionID                 string   `json:"connectionId,omitempty"`
	Weight                       *float64 `json:"weight,omitempty"`
	FallbackOnlyOnQuotaExhaustion *bool    `json:"fallbackOnlyOnQuotaExhaustion,omitempty"`
}

// Combo represents a combo entity in /api/combos
type Combo struct {
	ID          string      `json:"id,omitempty"`
	Name        string      `json:"name"`
	Strategy    string      `json:"strategy"`
	Description string      `json:"description,omitempty"`
	Models      []ComboStep `json:"models"`
	Config      any         `json:"config,omitempty"`
}

// CombosListResponse wraps the response when /api/combos returns an object with list of combos
type CombosListResponse struct {
	Combos []Combo `json:"combos,omitempty"`
	Data   []Combo `json:"data,omitempty"`
}

// UnmarshalCombos parses either a JSON array of Combo or an object with combos/data field
func UnmarshalCombos(data []byte) ([]Combo, error) {
	// Try parsing as array first
	var list []Combo
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}

	// Try parsing as object with combos or data field
	var resp CombosListResponse
	if err := json.Unmarshal(data, &resp); err == nil {
		if resp.Combos != nil {
			return resp.Combos, nil
		}
		if resp.Data != nil {
			return resp.Data, nil
		}
	}

	return nil, fmt.Errorf("unable to parse combos response: %s", string(data))
}

// UnmarshalModelEntries parses either a JSON object with data field or a raw array of ModelEntry
func UnmarshalModelEntries(data []byte) ([]ModelEntry, error) {
	var resp ModelListResponse
	if err := json.Unmarshal(data, &resp); err == nil && resp.Data != nil {
		return resp.Data, nil
	}

	var list []ModelEntry
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}

	return nil, fmt.Errorf("unable to parse model entries response: %s", string(data))
}

// ProviderTestBatchRequest payload for POST /api/providers/test-batch
type ProviderTestBatchRequest struct {
	Mode       string  `json:"mode"`
	ProviderID *string `json:"providerId"`
}

// ProviderTestDiagnosis carries diagnostic info for provider test
type ProviderTestDiagnosis struct {
	Type    string  `json:"type,omitempty"`
	Source  string  `json:"source,omitempty"`
	Message *string `json:"message,omitempty"`
	Code    *string `json:"code,omitempty"`
}

// ProviderTestResult represents a single provider's test result
type ProviderTestResult struct {
	Provider       string                 `json:"provider"`
	ConnectionID   string                 `json:"connectionId,omitempty"`
	ConnectionName string                 `json:"connectionName,omitempty"`
	AuthType       string                 `json:"authType,omitempty"`
	Valid          bool                   `json:"valid"`
	LatencyMs      int64                  `json:"latencyMs,omitempty"`
	Error          *string                `json:"error,omitempty"`
	Diagnosis      *ProviderTestDiagnosis `json:"diagnosis,omitempty"`
	StatusCode     *int                   `json:"statusCode,omitempty"`
	TestedAt       string                 `json:"testedAt,omitempty"`
}

// ProviderTestBatchSummary aggregates batch test stats
type ProviderTestBatchSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// ProviderTestBatchResponse represents response from POST /api/providers/test-batch
type ProviderTestBatchResponse struct {
	Mode       string                    `json:"mode,omitempty"`
	ProviderID *string                   `json:"providerId,omitempty"`
	Results    []ProviderTestResult      `json:"results"`
	TestedAt   string                    `json:"testedAt,omitempty"`
	Summary    *ProviderTestBatchSummary `json:"summary,omitempty"`
}

// ModelCompatOverride represents an override item in provider-models response
type ModelCompatOverride struct {
	ID               string          `json:"id"`
	IsHidden         *bool           `json:"isHidden,omitempty"`
	HiddenModalities map[string]bool `json:"hiddenModalities,omitempty"`
}

// ProviderModelsResponse represents response from GET /api/provider-models
type ProviderModelsResponse struct {
	Models                 []ModelEntry          `json:"models"`
	ModelCompatOverrides   []ModelCompatOverride `json:"modelCompatOverrides,omitempty"`
	HiddenModelsByProvider map[string][]string   `json:"hiddenModelsByProvider,omitempty"`
	EndpointAdapters       map[string]string     `json:"endpointAdapters,omitempty"`
}

// PatchProviderModelsBatchRequest payload for batch PATCH /api/provider-models
type PatchProviderModelsBatchRequest struct {
	IsHidden bool     `json:"isHidden"`
	ModelIDs []string `json:"modelIds"`
	Modality string   `json:"modality,omitempty"`
}

// PatchProviderModelRequest payload for single model PATCH /api/provider-models?provider=...&modelId=...
type PatchProviderModelRequest struct {
	IsHidden bool   `json:"isHidden"`
	Modality string `json:"modality,omitempty"`
}

// ModelTestRequest payload for POST /api/models/test
type ModelTestRequest struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

// ModelTestResponse represents response from POST /api/models/test
type ModelTestResponse struct {
	Status       string `json:"status"` // "ok" or "error"
	LatencyMs    int64  `json:"latencyMs,omitempty"`
	ResponseText string `json:"responseText,omitempty"`
	Error        string `json:"error,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
}

