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

	// 3. Load existing combo state to preserve working models and allow incremental updates
	var (
		currentSteps  []ComboStep
		targetComboID string
		comboExists   bool
	)

	existingCombos, listErr := sc.Client.ListCombos(ctx)
	if listErr != nil {
		log.Printf("[WARN] Unable to list combos (%v); will create combo if needed.", listErr)
	} else {
		for _, ex := range existingCombos {
			if strings.EqualFold(ex.Name, comboName) || (comboName != "" && strings.EqualFold(ex.ID, comboName)) {
				comboExists = true
				targetComboID = ex.ID
				if targetComboID == "" {
					targetComboID = ex.Name
				}
				currentSteps = append([]ComboStep{}, ex.Models...)
				log.Printf("[INFO] Found existing combo %q (id: %s) with %d initial models.", comboName, targetComboID, len(currentSteps))
				break
			}
		}
	}

	saveCombo := func() (string, error) {
		if dryRun {
			log.Printf("[INFO] [DRY RUN] Would update combo %q with %d models.", comboName, len(currentSteps))
			for i, step := range currentSteps {
				log.Printf("   [%02d] %s (provider: %s)", i+1, step.Model, step.ProviderID)
			}
			return "dry-run", nil
		}

		if len(currentSteps) == 0 {
			log.Printf("[WARN] Combo %q has 0 models remaining. Skipping update to avoid creating/leaving an empty combo.", comboName)
			return "skipped", nil
		}

		combo := Combo{
			ID:          comboName,
			Name:        comboName,
			Strategy:    strategy,
			Description: "Free models with tool calling and reasoning capabilities validated by combobreak",
			Models:      currentSteps,
		}

		if comboExists && targetComboID != "" {
			combo.ID = targetComboID
			updateErr := sc.Client.UpdateCombo(ctx, targetComboID, combo)
			if updateErr == nil {
				return "updated", nil
			}
			// If Update returned 404, maybe it was deleted upstream; fallback to create
			if strings.Contains(updateErr.Error(), "404") {
				if createErr := sc.Client.CreateCombo(ctx, combo); createErr == nil {
					comboExists = true
					return "created", nil
				}
			}
			return "", fmt.Errorf("failed to update combo %q (id: %s): %w", comboName, targetComboID, updateErr)
		}

		// Combo doesn't exist yet, create it
		createErr := sc.Client.CreateCombo(ctx, combo)
		if createErr == nil {
			comboExists = true
			targetComboID = combo.ID
			if targetComboID == "" {
				targetComboID = combo.Name
			}
			return "created", nil
		}

		// If Create returned conflict (already exists), update it
		if strings.Contains(createErr.Error(), "409") || strings.Contains(strings.ToLower(createErr.Error()), "already exists") {
			targetID := combo.ID
			if targetID == "" {
				targetID = combo.Name
			}
			if updateErr := sc.Client.UpdateCombo(ctx, targetID, combo); updateErr == nil {
				comboExists = true
				targetComboID = targetID
				return "updated", nil
			}
		}

		return "", fmt.Errorf("failed to create combo %q: %w", comboName, createErr)
	}

	// 4. Sequential processing of each active provider
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

		// Test each candidate model sequentially and update combo progressively
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

				// If model was present in combo, remove it immediately
				if newSteps, removed := removeStep(currentSteps, provider, modelID); removed {
					currentSteps = newSteps
					log.Printf("[WARN] [%s] Removed failed model %q from combo %q (%d models left). Updating combo in OmniRoute...",
						provider, modelID, comboName, len(currentSteps))
					act, err := saveCombo()
					if err != nil {
						log.Printf("[ERROR] Failed to update combo after removing %q: %v", modelID, err)
					} else {
						report.Action = act
					}
				}
				continue
			}

			if testResp.Status == "ok" {
				log.Printf("[INFO] [%s] Model %q PASSED (latency: %dms)", provider, modelID, testResp.LatencyMs)
				if idx, found := containsStep(currentSteps, provider, modelID); !found {
					// Discovered new model: append to combo and update immediately
					step := ComboStep{
						Model:      modelID,
						ProviderID: provider,
					}
					currentSteps = append(currentSteps, step)
					log.Printf("[INFO] [%s] Discovered new model %q. Added to combo %q (now %d models). Updating combo in OmniRoute...",
						provider, modelID, comboName, len(currentSteps))
					act, err := saveCombo()
					if err != nil {
						log.Printf("[ERROR] Failed to update combo after adding %q: %v", modelID, err)
					} else {
						report.Action = act
						log.Printf("[INFO] Combo %q successfully %s with %d models.", comboName, act, len(currentSteps))
					}
				} else {
					// Model was already present; backfill providerID if empty
					if currentSteps[idx].ProviderID == "" {
						currentSteps[idx].ProviderID = provider
					}
					log.Printf("[INFO] [%s] Model %q PASSED and is already active in combo %q (%d models).",
						provider, modelID, comboName, len(currentSteps))
				}
			} else {
				log.Printf("[WARN] [%s] Model %q FAILED test (status: %s, code: %d, err: %s). Hiding in OmniRoute...",
					provider, modelID, testResp.Status, testResp.StatusCode, testResp.Error)
				report.ModelsFailed = append(report.ModelsFailed, fmt.Sprintf("%s/%s", provider, modelID))
				if hideErr := sc.Client.HideProviderModel(ctx, provider, modelID); hideErr != nil {
					log.Printf("[WARN] [%s] Failed to hide model %q: %v", provider, modelID, hideErr)
				}

				// If model was present in combo, remove it immediately
				if newSteps, removed := removeStep(currentSteps, provider, modelID); removed {
					currentSteps = newSteps
					log.Printf("[WARN] [%s] Removed failed model %q from combo %q (%d models left). Updating combo in OmniRoute...",
						provider, modelID, comboName, len(currentSteps))
					act, err := saveCombo()
					if err != nil {
						log.Printf("[ERROR] Failed to update combo after removing %q: %v", modelID, err)
					} else {
						report.Action = act
					}
				}
			}
		}
	}

	report.ModelsApproved = currentSteps
	log.Printf("[INFO] Validation finished: %d tested, %d approved in combo, %d failed.",
		report.TotalModelsTested, len(report.ModelsApproved), len(report.ModelsFailed))

	if dryRun {
		report.Action = "dry-run"
		return report, nil
	}

	if len(currentSteps) == 0 && report.Action == "none" {
		log.Println("[WARN] No models passed live validation. Skipping combo modification to prevent service interruption.")
		report.Action = "skipped"
		return report, nil
	}

	if report.Action == "none" && len(currentSteps) > 0 {
		report.Action = "preserved"
	}

	return report, nil
}

// containsStep checks whether a model step already exists in the given slice
func containsStep(steps []ComboStep, provider, model string) (int, bool) {
	for i, s := range steps {
		if strings.EqualFold(s.Model, model) && strings.EqualFold(s.ProviderID, provider) {
			return i, true
		}
	}
	for i, s := range steps {
		if strings.EqualFold(s.Model, model) && s.ProviderID == "" {
			return i, true
		}
	}
	return -1, false
}

// removeStep removes all occurrences matching provider and model from steps
func removeStep(steps []ComboStep, provider, model string) ([]ComboStep, bool) {
	var remaining []ComboStep
	removed := false
	for _, s := range steps {
		isMatch := strings.EqualFold(s.Model, model) && (s.ProviderID == "" || strings.EqualFold(s.ProviderID, provider))
		if isMatch {
			removed = true
		} else {
			remaining = append(remaining, s)
		}
	}
	return remaining, removed
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
