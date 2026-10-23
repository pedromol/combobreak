package omniroute

import (
	"strings"
)

// FilterResult contains the detailed evaluation of a model
type FilterResult struct {
	Model       ModelEntry
	Passed      bool
	HasToolCall bool
	HasReason   bool
	IsFree      bool
	FreeReason  string
	RejectNotes []string
}

// ModelFilter encapsulates the logic to select eligible models for the combo
type ModelFilter struct {
	ExcludedComboName string
}

// NewModelFilter creates a filter instance
func NewModelFilter(excludedComboName string) *ModelFilter {
	return &ModelFilter{
		ExcludedComboName: strings.ToLower(excludedComboName),
	}
}

// HasToolCalling checks if the model supports tool/function calling
func (f *ModelFilter) HasToolCalling(m *ModelEntry) bool {
	if m.Capabilities == nil {
		return false
	}
	caps := m.Capabilities
	if caps.ToolCalling != nil && *caps.ToolCalling {
		return true
	}
	if caps.Tools != nil && *caps.Tools {
		return true
	}
	if caps.FunctionCalling != nil && *caps.FunctionCalling {
		return true
	}
	return false
}

// HasReasoning checks if the model has reasoning or thinking capability
func (f *ModelFilter) HasReasoning(m *ModelEntry) bool {
	if m.Capabilities == nil {
		return false
	}
	caps := m.Capabilities
	if caps.Reasoning != nil && *caps.Reasoning {
		return true
	}
	if caps.Thinking != nil && *caps.Thinking {
		return true
	}
	if caps.SupportsThinking != nil && *caps.SupportsThinking {
		return true
	}

	// Check effort_tiers if present
	if caps.EffortTiers != nil {
		switch v := caps.EffortTiers.(type) {
		case bool:
			if v {
				return true
			}
		case []any:
			if len(v) > 0 {
				return true
			}
		case []string:
			if len(v) > 0 {
				return true
			}
		}
	}

	return false
}

// IsFree checks whether the model is free either by its name/id or its reported cost
func (f *ModelFilter) IsFree(m *ModelEntry) (bool, string) {
	// 1. Check if explicitly marked free in name or ID or root
	idLower := strings.ToLower(m.ID)
	nameLower := strings.ToLower(m.Name)
	rootLower := strings.ToLower(m.Root)

	if strings.Contains(idLower, "free") {
		return true, "name/id contains 'free' (" + m.ID + ")"
	}
	if nameLower != "" && strings.Contains(nameLower, "free") {
		return true, "name contains 'free' (" + m.Name + ")"
	}
	if rootLower != "" && strings.Contains(rootLower, "free") {
		return true, "root contains 'free' (" + m.Root + ")"
	}

	// 2. Check pricing information
	if m.Pricing != nil {
		p := m.Pricing
		if p.IsFree != nil && *p.IsFree {
			return true, "pricing explicitly marked is_free=true"
		}

		// If input and output costs are provided and equal to 0
		if p.Input != nil && p.Output != nil {
			inputZero := *p.Input == 0
			outputZero := *p.Output == 0
			reasoningZero := p.Reasoning == nil || *p.Reasoning == 0
			if inputZero && outputZero && reasoningZero {
				return true, "pricing reported as 0 cost (input=0, output=0)"
			}
		}
	}

	return false, "no free indicator in name or pricing"
}

// Evaluate evaluates a single model against all criteria
func (f *ModelFilter) Evaluate(m *ModelEntry) FilterResult {
	res := FilterResult{
		Model: *m,
	}

	// Exclude empty IDs or the combo itself to prevent recursive routing
	idLower := strings.ToLower(m.ID)
	if idLower == "" {
		res.RejectNotes = append(res.RejectNotes, "empty model ID")
		return res
	}
	if f.ExcludedComboName != "" && (idLower == f.ExcludedComboName || strings.HasSuffix(idLower, "/"+f.ExcludedComboName)) {
		res.RejectNotes = append(res.RejectNotes, "matches combo name (prevent self-reference)")
		return res
	}
	if strings.EqualFold(m.OwnedBy, "combo") {
		res.RejectNotes = append(res.RejectNotes, "owned_by is 'combo'")
		return res
	}

	res.HasToolCall = f.HasToolCalling(m)
	if !res.HasToolCall {
		res.RejectNotes = append(res.RejectNotes, "missing tool calling capability")
	}

	res.HasReason = f.HasReasoning(m)
	if !res.HasReason {
		res.RejectNotes = append(res.RejectNotes, "missing reasoning capability")
	}

	free, reason := f.IsFree(m)
	res.IsFree = free
	res.FreeReason = reason
	if !res.IsFree {
		res.RejectNotes = append(res.RejectNotes, "not free (neither in name nor zero cost)")
	}

	res.Passed = res.HasToolCall && res.HasReason && res.IsFree
	return res
}

// Filter evaluates a list of models and returns only the matching ones
func (f *ModelFilter) Filter(models []ModelEntry) ([]ModelEntry, []FilterResult) {
	var passed []ModelEntry
	var allResults []FilterResult

	seen := make(map[string]bool)

	for _, m := range models {
		res := f.Evaluate(&m)
		allResults = append(allResults, res)

		if res.Passed {
			// Avoid duplicate model IDs
			if !seen[m.ID] {
				seen[m.ID] = true
				passed = append(passed, m)
			}
		}
	}

	return passed, allResults
}
