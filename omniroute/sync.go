package omniroute

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// SyncReport summarizes the results of a sync operation
type SyncReport struct {
	TotalProvidersChecked int
	ProvidersPassed       int
	TotalModelsTested     int
	ModelsApproved        []ComboStep
	ModelsFailed          []string
	Action                string
}

// SyncCoordinator orchestrates provider testing, model testing, and combo upserts
type SyncCoordinator struct {
	Client *Client
	Filter *ModelFilter
}

// NewSyncCoordinator creates a new coordinator
func NewSyncCoordinator(client *Client, filter *ModelFilter) *SyncCoordinator {
	return &SyncCoordinator{
		Client: client,
		Filter: filter,
	}
}

// Sync runs the full dynamic synchronization pipeline
func (sc *SyncCoordinator) Sync(ctx context.Context, comboName, strategy string, dryRun, verbose bool) (*SyncReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	report := &SyncReport{
		Action: "none",
	}

	// 1. Fetch catalog from /v1/models to build capability index
	catalogMap := make(map[string]ModelEntry)
	catalogModels, err := sc.Client.GetModels(ctx)
	if err != nil {
		log.Printf("[WARN] Unable to fetch /v1/models catalog (%v); capability checks will rely on provider metadata.", err)
	} else {
		for _, m := range catalogModels {
			idLower := strings.ToLower(m.ID)
			catalogMap[idLower] = m
			if idx := strings.LastIndex(idLower, "/"); idx >= 0 {
				catalogMap[idLower[idx+1:]] = m
			}
		}
		if verbose {
			log.Printf("[DEBUG] Indexed %d models from /v1/models catalog for capability validation.", len(catalogModels))
		}
	}

	// 2. Discover active providers via /api/providers/test-batch
	log.Println("[INFO] Testing provider connections via POST /api/providers/test-batch...")
	providerResults, err := sc.Client.TestBatchProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("provider batch test failed: %w", err)
	}

	report.TotalProvidersChecked = len(providerResults)
	var activeProviders []string
	for _, res := range providerResults {
		if res.Valid && res.Error == nil {
			activeProviders = append(activeProviders, res.Provider)
		} else {
			if verbose {
				errMsg := "unknown"
				if res.Error != nil {
					errMsg = *res.Error
				}
				log.Printf("[DEBUG] Skipping provider %q: valid=%t, error=%s", res.Provider, res.Valid, errMsg)
			}
		}
	}

	report.ProvidersPassed = len(activeProviders)
	log.Printf("[INFO] %d of %d providers passed health check: %v", len(activeProviders), len(providerResults), activeProviders)

	var approvedSteps []ComboStep
	seenModelKeys := make(map[string]bool)

	// 3. Sequential processing of each active provider
	for _, provider := range activeProviders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		log.Printf("[INFO] [%s] Triggering upstream catalog sync...", provider)
		if syncErr := sc.Client.SyncAvailableModels(ctx, provider); syncErr != nil {
			log.Printf("[WARN] [%s] Sychronization failed: %v. Continuing...", provider, syncErr)
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		log.Printf("[INFO] [%s] Fetching provider models...", provider)
		providerModelsResp, err := sc.Client.GetProviderModels(ctx, provider)
		if err != nil {
			log.Printf("[ERROR] [%s] Failed to fetch provider models: %v. Skipping provider.", provider, err)
			continue
		}

		// Collect candidate model IDs from models array and modelCompatOverrides
		candidates := sc.extractCandidateModels(provider, providerModelsResp, catalogMap, comboName, verbose)
		if len(candidates) == 0 {
			if verbose {
				log.Printf("[DEBUG] [%s] No models qualified after capability filtering.", provider)
			}
			continue
		}

		var candidateIDs []string
		for _, c := range candidates {
			candidateIDs = append(candidateIDs, c.ID)
		}

		log.Printf("[INFO] [%s] Unhiding %d candidate model(s) for chat...", provider, len(candidateIDs))
		if unhideErr := sc.Client.UnhideProviderModels(ctx, provider, candidateIDs); unhideErr != nil {
			log.Printf("[WARN] [%s] Failed to unhide models: %v. Attempting tests anyway.", provider, unhideErr)
		}

		// Test each candidate model sequentially
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			modelID := candidate.ID
			report.TotalModelsTested++

			log.Printf("[INFO] [%s] Testing model %q via POST /api/models/test...", provider, modelID)
			testResp, testErr := sc.Client.TestModel(ctx, provider, modelID)
			if testErr != nil {
				log.Printf("[WARN] [%s] Model %q transport error: %v. Treating as failed.", provider, modelID, testErr)
				report.ModelsFailed = append(report.ModelsFailed, fmt.Sprintf("%s/%s", provider, modelID))
				_ = sc.Client.HideProviderModel(ctx, provider, modelID)
				continue
			}

			if testResp.Status == "ok" {
				log.Printf("[INFO] [%s] Model %q PASSED (latency: %dms)", provider, modelID, testResp.LatencyMs)
				comboKey := fmt.Sprintf("%s:%s", provider, modelID)
				if !seenModelKeys[comboKey] {
					seenModelKeys[comboKey] = true
					step := ComboStep{
						Model:      modelID,
						ProviderID: provider,
					}
					approvedSteps = append(approvedSteps, step)
				}
			} else {
				log.Printf("[WARN] [%s] Model %q FAILED test (status: %s, code: %d, err: %s). Hiding in OmniRoute...",
					provider, modelID, testResp.Status, testResp.StatusCode, testResp.Error)
				report.ModelsFailed = append(report.ModelsFailed, fmt.Sprintf("%s/%s", provider, modelID))
				if hideErr := sc.Client.HideProviderModel(ctx, provider, modelID); hideErr != nil {
					log.Printf("[WARN] [%s] Failed to hide model %q: %v", provider, modelID, hideErr)
				}
			}
		}
	}

	report.ModelsApproved = approvedSteps
	log.Printf("[INFO] Validation finished: %d tested, %d approved, %d failed.",
		report.TotalModelsTested, len(report.ModelsApproved), len(report.ModelsFailed))

	if len(approvedSteps) == 0 {
		log.Println("[WARN] No models passed live validation. Skipping combo modification to prevent service interruption.")
		report.Action = "skipped"
		return report, nil
	}

	combo := Combo{
		ID:          comboName,
		Name:        comboName,
		Strategy:    strategy,
		Description: "Free models with tool calling and reasoning capabilities validated by combobreak",
		Models:      approvedSteps,
	}

	if dryRun {
		log.Printf("[INFO] [DRY RUN] Would create/update combo %q with %d models. Models:", comboName, len(approvedSteps))
		for i, step := range approvedSteps {
			log.Printf("   [%02d] %s (provider: %s)", i+1, step.Model, step.ProviderID)
		}
		report.Action = "dry-run"
		return report, nil
	}

	log.Printf("[INFO] Creating or updating combo %q in OmniRoute with %d models...", comboName, len(approvedSteps))
	action, err := sc.Client.UpsertCombo(ctx, combo)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert combo: %w", err)
	}

	report.Action = action
	log.Printf("[INFO] Combo %q successfully %s with %d models.", comboName, action, len(approvedSteps))
	return report, nil
}

// extractCandidateModels collects models from provider response and filters by tool calling & reasoning
func (sc *SyncCoordinator) extractCandidateModels(
	provider string,
	resp *ProviderModelsResponse,
	catalog map[string]ModelEntry,
	comboName string,
	verbose bool,
) []ModelEntry {
	seen := make(map[string]bool)
	var candidates []ModelEntry

	comboLower := strings.ToLower(comboName)

	addCandidate := func(id string, entry *ModelEntry) {
		idTrimmed := strings.TrimSpace(id)
		if idTrimmed == "" || seen[idTrimmed] {
			return
		}
		seen[idTrimmed] = true

		idLower := strings.ToLower(idTrimmed)
		// Exclude combo itself to prevent recursive routing loops
		if idLower == comboLower || strings.HasSuffix(idLower, "/"+comboLower) {
			return
		}

		// Resolve capabilities
		var caps *ModelCapabilities
		if entry != nil && entry.Capabilities != nil {
			caps = entry.Capabilities
		} else if catEntry, found := catalog[idLower]; found && catEntry.Capabilities != nil {
			caps = catEntry.Capabilities
		} else if catEntry, found := catalog[strings.ToLower(provider+"/"+idTrimmed)]; found && catEntry.Capabilities != nil {
			caps = catEntry.Capabilities
		}

		m := ModelEntry{
			ID:           idTrimmed,
			Capabilities: caps,
		}

		// Verify tool calling and reasoning capabilities when available
		if caps != nil {
			hasTools := sc.Filter.HasToolCalling(&m)
			hasReason := sc.Filter.HasReasoning(&m)
			if !hasTools || !hasReason {
				if verbose {
					log.Printf("[DEBUG] [%s] Excluding %q: tools=%t, reasoning=%t", provider, idTrimmed, hasTools, hasReason)
				}
				return
			}
		}

		candidates = append(candidates, m)
	}

	// 1. From models slice
	for _, m := range resp.Models {
		entry := m
		addCandidate(m.ID, &entry)
	}

	// 2. From modelCompatOverrides
	for _, ov := range resp.ModelCompatOverrides {
		addCandidate(ov.ID, nil)
	}

	// 3. From hiddenModelsByProvider
	if hiddenList, ok := resp.HiddenModelsByProvider[provider]; ok {
		for _, hid := range hiddenList {
			addCandidate(hid, nil)
		}
	}

	return candidates
}
