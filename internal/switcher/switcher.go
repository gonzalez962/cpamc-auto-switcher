package switcher

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// ListAccounts retrieves and evaluates all accounts for the configured provider(s) along with their quota.
func (s *Switcher) ListAccounts(ctx context.Context) ([]AccountState, error) {
	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	targetProviders := s.cfg.ResolvedProviders()
	providerSet := make(map[string]bool)
	for _, p := range targetProviders {
		providerSet[strings.ToLower(p)] = true
	}

	var accounts []AccountState

	for _, f := range files {
		fProvider := strings.ToLower(strings.TrimSpace(f.Provider))
		if !providerSet[fProvider] {
			continue
		}

		name := f.Name
		if name == "" {
			name = f.ID
		}

		meta, errMeta := s.client.GetAuthFileDetails(ctx, name)
		if errMeta != nil {
			return nil, fmt.Errorf("read metadata for %s: %w", name, errMeta)
		}

		activePrefix, reservePrefix := s.cfg.ConventionForProvider(f.Provider)

		state := AccountState{
			Entry:            f,
			Prefix:           meta.Prefix,
			ChatGPTAccountID: meta.ChatGPTAccountID,
			IsActive:         meta.Prefix == activePrefix,
			IsReserve:        strings.HasPrefix(meta.Prefix, reservePrefix),
		}

		// Fetch quota summary if not disabled
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

		accounts = append(accounts, state)
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
	var loadedFiles []fileWithMeta
	var targetItem *fileWithMeta

	for _, f := range files {
		name := f.Name
		if name == "" {
			name = f.ID
		}

		meta, errMeta := s.client.GetAuthFileDetails(ctx, name)
		if errMeta != nil {
			return nil, fmt.Errorf("read metadata for %s: %w", name, errMeta)
		}

		item := fileWithMeta{entry: f, meta: meta}
		loadedFiles = append(loadedFiles, item)

		// Match target by Prefix, ID, Name, or Email
		if strings.EqualFold(meta.Prefix, target) || strings.EqualFold(f.ID, target) || strings.EqualFold(f.Name, target) || strings.EqualFold(f.Email, target) {
			targetItem = &item
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
		// Target was not a reserve (e.g. had another prefix or none); find first free sequence index
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
			// Rollback target back to its original prefix
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

// RunProvider evaluates and rotates a single provider.
func (s *Switcher) RunProvider(ctx context.Context, provider string, dryRun bool) (*SwitchResult, error) {
	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	activePrefix, reservePrefixPrefix := s.cfg.ConventionForProvider(provider)
	var providerAccounts []AccountState

	// 1. Filter by provider and resolve prefixes
	for _, f := range files {
		if !strings.EqualFold(strings.TrimSpace(f.Provider), strings.TrimSpace(provider)) {
			continue
		}
		if f.Disabled {
			continue
		}

		name := f.Name
		if name == "" {
			name = f.ID
		}

		meta, errMeta := s.client.GetAuthFileDetails(ctx, name)
		if errMeta != nil {
			return nil, fmt.Errorf("read metadata for %s: %w", name, errMeta)
		}

		state := AccountState{
			Entry:            f,
			Prefix:           meta.Prefix,
			ChatGPTAccountID: meta.ChatGPTAccountID,
			IsActive:         meta.Prefix == activePrefix,
			IsReserve:        strings.HasPrefix(meta.Prefix, reservePrefixPrefix),
		}
		providerAccounts = append(providerAccounts, state)
	}

	if len(providerAccounts) == 0 {
		return &SwitchResult{
			Rotated: false,
			Reason:  fmt.Sprintf("no active accounts found for provider %q", provider),
		}, nil
	}

	// 2. Identify active account
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

	// 3. Fetch quota for active account
	accountOrProjectID := active.Entry.ProjectID
	if strings.EqualFold(provider, "codex") && active.ChatGPTAccountID != "" {
		accountOrProjectID = active.ChatGPTAccountID
	}

	quotaBytes, errQuota := s.client.GetQuotaSummaryForProvider(ctx, provider, active.Entry.AuthIndex, accountOrProjectID)
	if errQuota != nil {
		return nil, fmt.Errorf("fetch active account quota (%s, %s): %w", provider, active.Entry.ID, errQuota)
	}

	activeQuota, errParse := quota.ParseQuotaSummaryForProvider(provider, quotaBytes)
	if errParse != nil {
		return nil, fmt.Errorf("parse active account quota (%s, %s): %w", provider, active.Entry.ID, errParse)
	}
	active.Quota = activeQuota

	// 4. Evaluate threshold condition
	var quotaInfoParts []string
	if activeQuota.HasFiveHour && activeQuota.WorstFiveHour != nil {
		quotaInfoParts = append(quotaInfoParts, fmt.Sprintf("5h: %.1f%% rem (%.1f%% used)",
			activeQuota.WorstFiveHour.RemainingPercentage, activeQuota.WorstFiveHour.ConsumedPercentage))
	}
	if activeQuota.HasWeekly && activeQuota.WorstWeekly != nil {
		quotaInfoParts = append(quotaInfoParts, fmt.Sprintf("weekly: %.1f%% rem (%.1f%% used)",
			activeQuota.WorstWeekly.RemainingPercentage, activeQuota.WorstWeekly.ConsumedPercentage))
	}
	quotaSummaryStr := strings.Join(quotaInfoParts, ", ")
	if quotaSummaryStr == "" {
		quotaSummaryStr = fmt.Sprintf("min available: %.1f%%", activeQuota.MinAvailableRemaining())
	}

	shouldRotate, reason := activeQuota.ShouldRotate(s.cfg.FiveHourThreshold, s.cfg.WeeklyThreshold)
	if !shouldRotate {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: active.Entry.ID,
			Reason:        fmt.Sprintf("active account %s within limits [%s] (%s)", active.Entry.ID, quotaSummaryStr, reason),
		}, nil
	}

	if len(reserves) == 0 {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: active.Entry.ID,
			Reason:        fmt.Sprintf("rotation triggered (%s), but no reserve accounts (prefix %s*) available for provider %q", reason, reservePrefixPrefix, provider),
		}, nil
	}

	// 5. Fetch and evaluate quota for all reserve accounts
	type candidate struct {
		account   *AccountState
		minRemain float64
	}
	var eligible []candidate

	for _, res := range reserves {
		resAccountOrProjectID := res.Entry.ProjectID
		if strings.EqualFold(provider, "codex") && res.ChatGPTAccountID != "" {
			resAccountOrProjectID = res.ChatGPTAccountID
		}

		qBytes, err := s.client.GetQuotaSummaryForProvider(ctx, provider, res.Entry.AuthIndex, resAccountOrProjectID)
		if err != nil {
			res.QuotaErr = err
			continue
		}

		parsed, err := quota.ParseQuotaSummaryForProvider(provider, qBytes)
		if err != nil {
			res.QuotaErr = err
			continue
		}
		res.Quota = parsed

		// Candidate must not be already in breach of thresholds
		exceeded, _ := parsed.ShouldRotate(s.cfg.FiveHourThreshold, s.cfg.WeeklyThreshold)
		if exceeded {
			continue
		}

		minRem := parsed.MinAvailableRemaining()
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

	// 6. Rank candidates by highest minimum available remaining quota (Policy 3B)
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

	// 7. Execute 2-step direct swap (Policy 4A)
	nameReserve := bestReserve.Entry.Name
	if nameReserve == "" {
		nameReserve = bestReserve.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameReserve, activePrefix); err != nil {
		return nil, fmt.Errorf("failed to promote reserve account %s to active prefix %q: %w", nameReserve, activePrefix, err)
	}

	nameActive := active.Entry.Name
	if nameActive == "" {
		nameActive = active.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameActive, reserveOriginalPrefix); err != nil {
		_ = s.client.PatchAuthPrefix(ctx, nameReserve, reserveOriginalPrefix)
		return nil, fmt.Errorf("failed to demote previous active account %s to %q: %w (rollback attempted)", nameActive, reserveOriginalPrefix, err)
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

// Run executes the evaluation and switching workflow across all resolved providers.
func (s *Switcher) Run(ctx context.Context, dryRun bool) (*SwitchResult, error) {
	providers := s.cfg.ResolvedProviders()
	if len(providers) == 1 {
		return s.RunProvider(ctx, providers[0], dryRun)
	}

	var anyRotated bool
	var reasons []string
	var lastActiveAccount string
	var lastSelectedReserve string

	for _, p := range providers {
		res, err := s.RunProvider(ctx, p, dryRun)
		if err != nil {
			return nil, fmt.Errorf("provider %s rotation failed: %w", p, err)
		}
		if res.Rotated {
			anyRotated = true
			lastActiveAccount = res.ActiveAccount
			lastSelectedReserve = res.SelectedReserve
		}
		reasons = append(reasons, fmt.Sprintf("[%s] %s", p, res.Reason))
	}

	return &SwitchResult{
		Rotated:         anyRotated,
		ActiveAccount:   lastActiveAccount,
		SelectedReserve: lastSelectedReserve,
		Reason:          strings.Join(reasons, " | "),
	}, nil
}
