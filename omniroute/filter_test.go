package omniroute

import (
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

func floatPtr(f float64) *float64 {
	return &f
}

func TestFilterModel_SuccessCases(t *testing.T) {
	filter := NewModelFilter("combobreak")

	tests := []struct {
		name       string
		model      ModelEntry
		wantFreeRe string
	}{
		{
			name: "Model with :free suffix and standard capabilities",
			model: ModelEntry{
				ID: "openrouter/meta-llama/llama-3.3-70b-instruct:free",
				Capabilities: &ModelCapabilities{
					ToolCalling: boolPtr(true),
					Reasoning:   boolPtr(true),
				},
			},
			wantFreeRe: "name/id contains 'free'",
		},
		{
			name: "Model with thinking=true and 0 cost pricing",
			model: ModelEntry{
				ID: "provider/qwen-2.5-coder",
				Capabilities: &ModelCapabilities{
					Tools:    boolPtr(true),
					Thinking: boolPtr(true),
				},
				Pricing: &ModelPricing{
					Input:  floatPtr(0),
					Output: floatPtr(0),
				},
			},
			wantFreeRe: "pricing reported as 0 cost",
		},
		{
			name: "Model with effort_tiers and pricing is_free=true",
			model: ModelEntry{
				ID: "provider/deepseek-r1",
				Capabilities: &ModelCapabilities{
					FunctionCalling: boolPtr(true),
					EffortTiers:     []string{"low", "medium", "high"},
				},
				Pricing: &ModelPricing{
					IsFree: boolPtr(true),
				},
			},
			wantFreeRe: "pricing explicitly marked is_free=true",
		},
		{
			name: "Model with supportsThinking=true and free in root",
			model: ModelEntry{
				ID:   "custom/free-thinker",
				Root: "provider/free-model",
				Capabilities: &ModelCapabilities{
					ToolCalling:      boolPtr(true),
					SupportsThinking: boolPtr(true),
				},
			},
			wantFreeRe: "name/id contains 'free'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := filter.Evaluate(&tt.model)
			if !res.Passed {
				t.Fatalf("expected model to pass, but failed with notes: %v", res.RejectNotes)
			}
			if !res.HasToolCall {
				t.Errorf("expected HasToolCall=true")
			}
			if !res.HasReason {
				t.Errorf("expected HasReason=true")
			}
			if !res.IsFree {
				t.Errorf("expected IsFree=true")
			}
		})
	}
}

func TestFilterModel_Rejections(t *testing.T) {
	filter := NewModelFilter("combobreak")

	tests := []struct {
		name        string
		model       ModelEntry
		expectCause string
	}{
		{
			name: "Missing tool calling",
			model: ModelEntry{
				ID: "provider/deepseek-r1:free",
				Capabilities: &ModelCapabilities{
					ToolCalling: boolPtr(false),
					Reasoning:   boolPtr(true),
				},
			},
			expectCause: "missing tool calling capability",
		},
		{
			name: "Missing reasoning",
			model: ModelEntry{
				ID: "provider/gpt-4o-mini:free",
				Capabilities: &ModelCapabilities{
					ToolCalling: boolPtr(true),
					Reasoning:   boolPtr(false),
				},
			},
			expectCause: "missing reasoning capability",
		},
		{
			name: "Not free (paid pricing, no free in name)",
			model: ModelEntry{
				ID: "provider/claude-3-7-sonnet",
				Capabilities: &ModelCapabilities{
					ToolCalling: boolPtr(true),
					Reasoning:   boolPtr(true),
				},
				Pricing: &ModelPricing{
					Input:  floatPtr(3.0),
					Output: floatPtr(15.0),
				},
			},
			expectCause: "not free",
		},
		{
			name: "Self reference combo rejected",
			model: ModelEntry{
				ID: "combobreak",
				Capabilities: &ModelCapabilities{
					ToolCalling: boolPtr(true),
					Reasoning:   boolPtr(true),
				},
			},
			expectCause: "matches combo name",
		},
		{
			name: "Owned by combo rejected",
			model: ModelEntry{
				ID:      "auto/free-coding",
				OwnedBy: "combo",
				Capabilities: &ModelCapabilities{
					ToolCalling: boolPtr(true),
					Reasoning:   boolPtr(true),
				},
			},
			expectCause: "owned_by is 'combo'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := filter.Evaluate(&tt.model)
			if res.Passed {
				t.Fatalf("expected model to be rejected, but it passed")
			}
			found := false
			for _, note := range res.RejectNotes {
				if note != "" && (note == tt.expectCause || (len(tt.expectCause) > 0 && len(note) >= len(tt.expectCause) && note[:len(tt.expectCause)] == tt.expectCause)) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected rejection note containing %q, got %v", tt.expectCause, res.RejectNotes)
			}
		})
	}
}
