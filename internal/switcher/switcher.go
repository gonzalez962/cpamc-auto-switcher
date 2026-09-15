package switcher

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
	"cpamc-auto-switcher/internal/quota"
)

// AccountState represents a discovered credential and its quota metrics.
type AccountState struct {
	Entry            client.AuthFileEntry
	Prefix           string
	ChatGPTAccountID string
	Quota            *quota.AccountQuota
	QuotaErr         error
	IsActive         bool
	IsReserve        bool
}

// SwitchResult summarizes execution output.
type SwitchResult struct {
	Rotated         bool   `json:"rotated"`
	ActiveAccount   string `json:"active_account"`
	SelectedReserve string `json:"selected_reserve,omitempty"`
	Reason          string `json:"reason"`
}

// Switcher coordinates account inspection, evaluation, and prefix swapping.
type Switcher struct {
	cfg    *config.Config
	client *client.Client
}

// New creates a new Switcher instance.
func New(cfg *config.Config, cli *client.Client) *Switcher {
	return &Switcher{
		cfg:    cfg,
		client: cli,
	}
}

// fetchAccountsConcurrently loads account metadata and quotas in parallel.
// It guarantees that all asynchronous API calls finish before returning.
func (s *Switcher) fetchAccountsConcurrently(ctx context.Context, targetProviders []string, skipDisabled bool) ([]AccountState, error) {
	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	providerSet := make(map[string]bool)
	for _, p := range targetProviders {
		providerSet[strings.ToLower(strings.TrimSpace(p))] = true
	}

	var matchingFiles []client.AuthFileEntry
	for _, f := range files {
		fProvider := strings.ToLower(strings.TrimSpace(f.Provider))
		if !providerSet[fProvider] {
			continue
		}
		if skipDisabled && f.Disabled {
			continue
		}
		matchingFiles = append(matchingFiles, f)
	}

	if len(matchingFiles) == 0 {
		return nil, nil
	}

	results := make([]AccountState, len(matchingFiles))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstFatalErr error

	// Concurrency limiter to avoid exhausting sockets or overloading proxies
	concurrencyLimit := 10
	if len(matchingFiles) < concurrencyLimit {
		concurrencyLimit = len(matchingFiles)
	}
	sem := make(chan struct{}, concurrencyLimit)

	for i := range matchingFiles {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstFatalErr == nil {
					firstFatalErr = ctx.Err()
				}
				mu.Unlock()
				return
			}

			f := matchingFiles[idx]
			name := f.Name
			if name == "" {
				name = f.ID
			}

			meta, errMeta := s.client.GetAuthFileDetails(ctx, name)
			if errMeta != nil {
				mu.Lock()
				if firstFatalErr == nil {
					firstFatalErr = fmt.Errorf("read metadata for %s: %w", name, errMeta)
				}
				mu.Unlock()
				return
			}

			activePrefix, reservePrefix := s.cfg.ConventionForProvider(f.Provider)

			state := AccountState{
				Entry:            f,
				Prefix:           meta.Prefix,
				ChatGPTAccountID: meta.ChatGPTAccountID,
				IsActive:         meta.Prefix == activePrefix,
				IsReserve:        strings.HasPrefix(meta.Prefix, reservePrefix),
			}

			// Fetch quota asynchronously if not disabled
			if !f.Disabled {
				accountOrProjectID := f.ProjectID
				if strings.EqualFold(f.Provider, "codex") && meta.ChatGPTAccountID != "" {
					accountOrProjectID = meta.ChatGPTAccountID
				}

				qBytes, errQuota := s.client.GetQuotaSummaryForProvider(ctx, f.Provider, f.AuthIndex, accountOrProjectID)
				if errQuota != nil {
					state.QuotaErr = errQuota
				} else {
					parsedQuota, errParse := quota.ParseQuotaSummaryForProvider(f.Provider, qBytes)
					if errParse != nil {
						state.QuotaErr = errParse
					} else {
						state.Quota = parsedQuota
					}
				}
			}

			mu.Lock()
			results[idx] = state
			mu.Unlock()
		}(i)
	}

	// Wait until ALL accounts and quotas have finished loading
	wg.Wait()

	if firstFatalErr != nil {
		return nil, firstFatalErr
	}

	return results, nil
}

// ListAccounts retrieves and evaluates all accounts for the configured provider(s) along with their quota.
func (s *Switcher) ListAccounts(ctx context.Context) ([]AccountState, error) {
	accounts, err := s.fetchAccountsConcurrently(ctx, s.cfg.ResolvedProviders(), false)
	if err != nil {
		return nil, err
	}

	// Sort accounts: provider alphabetically, active first, then by prefix alphabetically, then by ID
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Entry.Provider != accounts[j].Entry.Provider {
			return accounts[i].Entry.Provider < accounts[j].Entry.Provider
		}
		if accounts[i].IsActive != accounts[j].IsActive {
			return accounts[i].IsActive
		}
		if accounts[i].Prefix != accounts[j].Prefix {
			return accounts[i].Prefix < accounts[j].Prefix
		}
		return accounts[i].Entry.ID < accounts[j].Entry.ID
	})

	return accounts, nil
}

// SwitchToAccount manually sets targetAccount to be the active account for its provider.
func (s *Switcher) SwitchToAccount(ctx context.Context, targetAccount string, dryRun bool) (*SwitchResult, error) {
	target := strings.TrimSpace(targetAccount)
	if target == "" {
		return nil, fmt.Errorf("target account identifier cannot be empty")
	}

	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	type fileWithMeta struct {
		entry client.AuthFileEntry
		meta  client.AuthFileMetadata
	}
	loadedFiles := make([]fileWithMeta, len(files))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	sem := make(chan struct{}, 10)
	for i := range files {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}

			f := files[idx]
			name := f.Name
			if name == "" {
				name = f.ID
			}

			meta, errMeta := s.client.GetAuthFileDetails(ctx, name)
			if errMeta != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("read metadata for %s: %w", name, errMeta)
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			loadedFiles[idx] = fileWithMeta{entry: f, meta: meta}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	var targetItem *fileWithMeta
	for i := range loadedFiles {
		item := &loadedFiles[i]
		if strings.EqualFold(item.meta.Prefix, target) || strings.EqualFold(item.entry.ID, target) || strings.EqualFold(item.entry.Name, target) || strings.EqualFold(item.entry.Email, target) {
			targetItem = item
			break
		}
	}

	if targetItem == nil {
		return nil, fmt.Errorf("account matching %q not found", target)
	}

	provider := targetItem.entry.Provider
	activePrefix, reservePrefixPrefix := s.cfg.ConventionForProvider(provider)

	var activeItem *fileWithMeta
	existingReserveIndices := make(map[int]bool)

	for i := range loadedFiles {
		item := &loadedFiles[i]
		if !strings.EqualFold(item.entry.Provider, provider) {
			continue
		}

		if item.meta.Prefix == activePrefix {
			activeItem = item
		}
		if strings.HasPrefix(item.meta.Prefix, reservePrefixPrefix) {
			var idx int
			numPart := strings.TrimPrefix(item.meta.Prefix, reservePrefixPrefix)
			if _, errScan := fmt.Sscanf(numPart, "%d", &idx); errScan == nil {
				existingReserveIndices[idx] = true
			}
		}
	}

	if targetItem.meta.Prefix == activePrefix {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: targetItem.entry.ID,
			Reason:        fmt.Sprintf("account %s is already active with prefix %q for provider %q", targetItem.entry.ID, activePrefix, provider),
		}, nil
	}

	// Determine new reserve prefix for the demoted active account
	newReservePrefix := targetItem.meta.Prefix
	if !strings.HasPrefix(newReservePrefix, reservePrefixPrefix) {
		nextIdx := 1
		for existingReserveIndices[nextIdx] {
			nextIdx++
		}
		newReservePrefix = fmt.Sprintf("%s%d", reservePrefixPrefix, nextIdx)
	}

	if dryRun {
		return &SwitchResult{
			Rotated:         true,
			ActiveAccount:   targetItem.entry.ID,
			SelectedReserve: targetItem.meta.Prefix,
			Reason: fmt.Sprintf("[DRY-RUN] Would promote %s to active (%s) and demote current active to %s (provider: %s)",
				targetItem.entry.ID, activePrefix, newReservePrefix, provider),
		}, nil
	}

	// Step 1: Promote target account to active prefix
	nameTarget := targetItem.entry.Name
	if nameTarget == "" {
		nameTarget = targetItem.entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameTarget, activePrefix); err != nil {
		return nil, fmt.Errorf("failed to promote account %s to %q: %w", nameTarget, activePrefix, err)
	}

	// Step 2: Demote previous active account if one existed
	if activeItem != nil {
		nameActive := activeItem.entry.Name
		if nameActive == "" {
			nameActive = activeItem.entry.ID
		}
		if err := s.client.PatchAuthPrefix(ctx, nameActive, newReservePrefix); err != nil {
			_ = s.client.PatchAuthPrefix(ctx, nameTarget, targetItem.meta.Prefix)
			return nil, fmt.Errorf("failed to demote active account %s to %q: %w (rollback attempted)", nameActive, newReservePrefix, err)
		}
	}

	return &SwitchResult{
		Rotated:         true,
		ActiveAccount:   targetItem.entry.ID,
		SelectedReserve: targetItem.meta.Prefix,
		Reason: fmt.Sprintf("manually promoted %s to active (%s); previous active assigned prefix %s (provider: %s)",
			targetItem.entry.ID, activePrefix, newReservePrefix, provider),
	}, nil
}

// RunProvider evaluates and rotates a single provider after ALL accounts' quotas have finished loading asynchronously.
func (s *Switcher) RunProvider(ctx context.Context, provider string, dryRun bool) (*SwitchResult, error) {
	activePrefix, reservePrefixPrefix := s.cfg.ConventionForProvider(provider)

	// Step 1: Concurrently load all account metadata and quotas for this provider
	providerAccounts, err := s.fetchAccountsConcurrently(ctx, []string{provider}, true)
	if err != nil {
		return nil, fmt.Errorf("fetch accounts for %s: %w", provider, err)
	}

	if len(providerAccounts) == 0 {
		return &SwitchResult{
			Rotated: false,
			Reason:  fmt.Sprintf("no active accounts found for provider %q", provider),
		}, nil
	}

	// Step 2: Identify active account and reserve accounts
	var active *AccountState
	var reserves []*AccountState

	for i := range providerAccounts {
		acc := &providerAccounts[i]
		if acc.IsActive {
			if active != nil {
				return nil, fmt.Errorf("multiple active accounts found with prefix %q for provider %s: %s and %s",
					activePrefix, provider, active.Entry.ID, acc.Entry.ID)
			}
			active = acc
		} else if acc.IsReserve {
			reserves = append(reserves, acc)
		}
	}

	if active == nil {
		return &SwitchResult{
			Rotated: false,
			Reason:  fmt.Sprintf("no account currently has active prefix %q for provider %q", activePrefix, provider),
		}, nil
	}

	if active.QuotaErr != nil {
		return nil, fmt.Errorf("active account quota check failed (%s, %s): %w", provider, active.Entry.ID, active.QuotaErr)
	}
	if active.Quota == nil {
		return nil, fmt.Errorf("active account quota missing (%s, %s)", provider, active.Entry.ID)
	}

	// Step 3: Evaluate threshold condition on active account
	var quotaInfoParts []string
	if active.Quota.HasFiveHour && active.Quota.WorstFiveHour != nil {
		quotaInfoParts = append(quotaInfoParts, fmt.Sprintf("5h %.1f%%", active.Quota.WorstFiveHour.ConsumedPercentage))
	}
	if active.Quota.HasWeekly && active.Quota.WorstWeekly != nil {
		quotaInfoParts = append(quotaInfoParts, fmt.Sprintf("weekly %.1f%%", active.Quota.WorstWeekly.ConsumedPercentage))
	}
	quotaSummaryStr := strings.Join(quotaInfoParts, ", ")
	if quotaSummaryStr == "" {
		quotaSummaryStr = fmt.Sprintf("%.1f%%", 100.0-active.Quota.MinAvailableRemaining())
	}

	shouldRotate, reason := active.Quota.ShouldRotate(s.cfg.FiveHourThreshold, s.cfg.WeeklyThreshold)
	if !shouldRotate {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: active.Entry.ID,
			Reason:        quotaSummaryStr,
		}, nil
	}

	if len(reserves) == 0 {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: active.Entry.ID,
			Reason:        fmt.Sprintf("rotation triggered (%s), but no reserve accounts (prefix %s*) available for provider %q", reason, reservePrefixPrefix, provider),
		}, nil
	}

	// Step 4: Evaluate reserve candidates (all quotas are ALREADY loaded in memory)
	type candidate struct {
		account   *AccountState
		minRemain float64
	}
	var eligible []candidate

	for _, res := range reserves {
		if res.QuotaErr != nil || res.Quota == nil {
			// Skip reserves with quota errors
			continue
		}

		// Candidate must not be already in breach of thresholds
		exceeded, _ := res.Quota.ShouldRotate(s.cfg.FiveHourThreshold, s.cfg.WeeklyThreshold)
		if exceeded {
			continue
		}

		minRem := res.Quota.MinAvailableRemaining()
		eligible = append(eligible, candidate{
			account:   res,
			minRemain: minRem,
		})
	}

	if len(eligible) == 0 {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: active.Entry.ID,
			Reason:        fmt.Sprintf("rotation triggered (%s), but all reserves exceed thresholds or failed quota check for provider %q", reason, provider),
		}, nil
	}

	// Step 5: Rank candidates by highest minimum available remaining quota (Policy 3B)
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].minRemain != eligible[j].minRemain {
			return eligible[i].minRemain > eligible[j].minRemain
		}
		return eligible[i].account.Entry.ID < eligible[j].account.Entry.ID
	})

	bestReserve := eligible[0].account
	reserveOriginalPrefix := bestReserve.Prefix

	if dryRun {
		return &SwitchResult{
			Rotated:         true,
			ActiveAccount:   active.Entry.ID,
			SelectedReserve: bestReserve.Entry.ID,
			Reason: fmt.Sprintf("[DRY-RUN] Would swap %s (%s) <-> %s (%s) (reason: %s, reserve min available: %.1f%%, provider: %s)",
				active.Entry.ID, activePrefix, bestReserve.Entry.ID, reserveOriginalPrefix, reason, eligible[0].minRemain, provider),
		}, nil
	}

	// Step 6: Execute 2-step direct swap (Policy 4A)
	nameReserve := bestReserve.Entry.Name
	if nameReserve == "" {
		nameReserve = bestReserve.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameReserve, activePrefix); err != nil {
		return nil, fmt.Errorf("failed to promote reserve %s to %q: %w", nameReserve, activePrefix, err)
	}

	nameActive := active.Entry.Name
	if nameActive == "" {
		nameActive = active.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameActive, reserveOriginalPrefix); err != nil {
		_ = s.client.PatchAuthPrefix(ctx, nameReserve, reserveOriginalPrefix)
		return nil, fmt.Errorf("failed to demote active %s to %q: %w (rollback attempted)", nameActive, reserveOriginalPrefix, err)
	}

	return &SwitchResult{
		Rotated:         true,
		ActiveAccount:   bestReserve.Entry.ID,
		SelectedReserve: active.Entry.ID,
		Reason: fmt.Sprintf("swapped %s (%s -> %s) and %s (%s -> %s) due to: %s (provider: %s)",
			bestReserve.Entry.ID, reserveOriginalPrefix, activePrefix,
			active.Entry.ID, activePrefix, reserveOriginalPrefix,
			reason, provider),
	}, nil
}

// Run executes the evaluation and switching workflow across all resolved providers asynchronously.
func (s *Switcher) Run(ctx context.Context, dryRun bool) (*SwitchResult, error) {
	providers := s.cfg.ResolvedProviders()
	if len(providers) == 1 {
		res, err := s.RunProvider(ctx, providers[0], dryRun)
		if err != nil {
			return nil, err
		}
		if !res.Rotated {
			res.Reason = fmt.Sprintf("%s: %s", providers[0], res.Reason)
		}
		return res, nil
	}

	type providerOutcome struct {
		provider string
		result   *SwitchResult
		err      error
	}

	outcomesChan := make(chan providerOutcome, len(providers))
	var wg sync.WaitGroup

	for _, p := range providers {
		wg.Add(1)
		go func(prov string) {
			defer wg.Done()
			res, err := s.RunProvider(ctx, prov, dryRun)
			outcomesChan <- providerOutcome{provider: prov, result: res, err: err}
		}(p)
	}

	wg.Wait()
	close(outcomesChan)

	outcomeMap := make(map[string]providerOutcome)
	for outcome := range outcomesChan {
		if outcome.err != nil {
			return nil, fmt.Errorf("provider %s rotation failed: %w", outcome.provider, outcome.err)
		}
		outcomeMap[outcome.provider] = outcome
	}

	var anyRotated bool
	var reasons []string
	var lastActiveAccount string
	var lastSelectedReserve string

	for _, p := range providers {
		outcome := outcomeMap[p]
		if outcome.result.Rotated {
			anyRotated = true
			lastActiveAccount = outcome.result.ActiveAccount
			lastSelectedReserve = outcome.result.SelectedReserve
			reasons = append(reasons, fmt.Sprintf("%s: %s", p, outcome.result.Reason))
		} else {
			reasons = append(reasons, fmt.Sprintf("%s: %s", p, outcome.result.Reason))
		}
	}

	return &SwitchResult{
		Rotated:         anyRotated,
		ActiveAccount:   lastActiveAccount,
		SelectedReserve: lastSelectedReserve,
		Reason:          strings.Join(reasons, " | "),
	}, nil
}
