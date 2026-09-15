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
	Entry     client.AuthFileEntry
	Prefix    string
	Quota     *quota.AccountQuota
	QuotaErr  error
	IsActive  bool
	IsReserve bool
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

// ListAccounts retrieves and evaluates all accounts for the configured provider along with their quota.
func (s *Switcher) ListAccounts(ctx context.Context) ([]AccountState, error) {
	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	var accounts []AccountState

	for _, f := range files {
		if !strings.EqualFold(strings.TrimSpace(f.Provider), strings.TrimSpace(s.cfg.Provider)) {
			continue
		}

		name := f.Name
		if name == "" {
			name = f.ID
		}

		prefix, errPrefix := s.client.GetAuthFilePrefix(ctx, name)
		if errPrefix != nil {
			return nil, fmt.Errorf("read prefix for %s: %w", name, errPrefix)
		}

		state := AccountState{
			Entry:     f,
			Prefix:    prefix,
			IsActive:  prefix == s.cfg.ActivePrefix,
			IsReserve: strings.HasPrefix(prefix, s.cfg.ReservePrefixPrefix),
		}

		// Fetch quota summary if not disabled
		if !f.Disabled {
			qBytes, errQuota := s.client.GetQuotaSummary(ctx, f.AuthIndex, f.ProjectID)
			if errQuota != nil {
				state.QuotaErr = errQuota
			} else {
				parsedQuota, errParse := quota.ParseQuotaSummary(qBytes)
				if errParse != nil {
					state.QuotaErr = errParse
				} else {
					state.Quota = parsedQuota
				}
			}
		}

		accounts = append(accounts, state)
	}

	// Sort accounts: active first, then by prefix alphabetically, then by ID
	sort.Slice(accounts, func(i, j int) bool {
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

// SwitchToAccount manually sets targetAccount to be the active account (prefix: agy).
func (s *Switcher) SwitchToAccount(ctx context.Context, targetAccount string, dryRun bool) (*SwitchResult, error) {
	target := strings.TrimSpace(targetAccount)
	if target == "" {
		return nil, fmt.Errorf("target account identifier cannot be empty")
	}

	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	var targetState *AccountState
	var activeState *AccountState
	existingReserveIndices := make(map[int]bool)

	for _, f := range files {
		if !strings.EqualFold(strings.TrimSpace(f.Provider), strings.TrimSpace(s.cfg.Provider)) {
			continue
		}

		name := f.Name
		if name == "" {
			name = f.ID
		}

		prefix, errPrefix := s.client.GetAuthFilePrefix(ctx, name)
		if errPrefix != nil {
			return nil, fmt.Errorf("read prefix for %s: %w", name, errPrefix)
		}

		state := AccountState{
			Entry:     f,
			Prefix:    prefix,
			IsActive:  prefix == s.cfg.ActivePrefix,
			IsReserve: strings.HasPrefix(prefix, s.cfg.ReservePrefixPrefix),
		}

		if state.IsActive {
			activeState = &state
		}

		if state.IsReserve {
			var idx int
			numPart := strings.TrimPrefix(prefix, s.cfg.ReservePrefixPrefix)
			if _, errScan := fmt.Sscanf(numPart, "%d", &idx); errScan == nil {
				existingReserveIndices[idx] = true
			}
		}

		// Match target by Prefix, ID, Name, or Email
		if strings.EqualFold(prefix, target) || strings.EqualFold(f.ID, target) || strings.EqualFold(f.Name, target) || strings.EqualFold(f.Email, target) {
			targetState = &state
		}
	}

	if targetState == nil {
		return nil, fmt.Errorf("account matching %q not found for provider %q", target, s.cfg.Provider)
	}

	if targetState.IsActive {
		return &SwitchResult{
			Rotated:       false,
			ActiveAccount: targetState.Entry.ID,
			Reason:        fmt.Sprintf("account %s is already active with prefix %q", targetState.Entry.ID, s.cfg.ActivePrefix),
		}, nil
	}

	// Determine new reserve prefix for the demoted active account
	newReservePrefix := targetState.Prefix
	if !strings.HasPrefix(newReservePrefix, s.cfg.ReservePrefixPrefix) {
		// Target was not a reserve (e.g. had another prefix or none); find first free sequence index
		nextIdx := 1
		for existingReserveIndices[nextIdx] {
			nextIdx++
		}
		newReservePrefix = fmt.Sprintf("%s%d", s.cfg.ReservePrefixPrefix, nextIdx)
	}

	if dryRun {
		return &SwitchResult{
			Rotated:         true,
			ActiveAccount:   targetState.Entry.ID,
			SelectedReserve: targetState.Prefix,
			Reason: fmt.Sprintf("[DRY-RUN] Would promote %s to active (%s) and demote current active to %s",
				targetState.Entry.ID, s.cfg.ActivePrefix, newReservePrefix),
		}, nil
	}

	// Step 1: Promote target account to active prefix
	nameTarget := targetState.Entry.Name
	if nameTarget == "" {
		nameTarget = targetState.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameTarget, s.cfg.ActivePrefix); err != nil {
		return nil, fmt.Errorf("failed to promote account %s to %q: %w", nameTarget, s.cfg.ActivePrefix, err)
	}

	// Step 2: Demote previous active account if one existed
	if activeState != nil {
		nameActive := activeState.Entry.Name
		if nameActive == "" {
			nameActive = activeState.Entry.ID
		}
		if err := s.client.PatchAuthPrefix(ctx, nameActive, newReservePrefix); err != nil {
			// Rollback target back to its original prefix
			_ = s.client.PatchAuthPrefix(ctx, nameTarget, targetState.Prefix)
			return nil, fmt.Errorf("failed to demote active account %s to %q: %w (rollback attempted)", nameActive, newReservePrefix, err)
		}
	}

	return &SwitchResult{
		Rotated:         true,
		ActiveAccount:   targetState.Entry.ID,
		SelectedReserve: targetState.Prefix,
		Reason: fmt.Sprintf("manually promoted %s to active (%s); previous active assigned prefix %s",
			targetState.Entry.ID, s.cfg.ActivePrefix, newReservePrefix),
	}, nil
}

// Run executes the evaluation and switching workflow.
func (s *Switcher) Run(ctx context.Context, dryRun bool) (*SwitchResult, error) {
	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}

	var providerAccounts []AccountState

	// 1. Filter by provider and resolve prefixes
	for _, f := range files {
		if !strings.EqualFold(strings.TrimSpace(f.Provider), strings.TrimSpace(s.cfg.Provider)) {
			continue
		}
		if f.Disabled {
			continue
		}

		name := f.Name
		if name == "" {
			name = f.ID
		}

		prefix, errPrefix := s.client.GetAuthFilePrefix(ctx, name)
		if errPrefix != nil {
			return nil, fmt.Errorf("read prefix for %s: %w", name, errPrefix)
		}

		state := AccountState{
			Entry:     f,
			Prefix:    prefix,
			IsActive:  prefix == s.cfg.ActivePrefix,
			IsReserve: strings.HasPrefix(prefix, s.cfg.ReservePrefixPrefix),
		}
		providerAccounts = append(providerAccounts, state)
	}

	if len(providerAccounts) == 0 {
		return &SwitchResult{
			Rotated: false,
			Reason:  fmt.Sprintf("no active accounts found for provider %q", s.cfg.Provider),
		}, nil
	}

	// 2. Identify active account
	var active *AccountState
	var reserves []*AccountState

	for i := range providerAccounts {
		acc := &providerAccounts[i]
		if acc.IsActive {
			if active != nil {
				return nil, fmt.Errorf("multiple active accounts found with prefix %q: %s and %s",
					s.cfg.ActivePrefix, active.Entry.ID, acc.Entry.ID)
			}
			active = acc
		} else if acc.IsReserve {
			reserves = append(reserves, acc)
		}
	}

	if active == nil {
		return &SwitchResult{
			Rotated: false,
			Reason:  fmt.Sprintf("no account currently has active prefix %q", s.cfg.ActivePrefix),
		}, nil
	}

	// 3. Fetch quota for active account
	quotaBytes, errQuota := s.client.GetQuotaSummary(ctx, active.Entry.AuthIndex, active.Entry.ProjectID)
	if errQuota != nil {
		return nil, fmt.Errorf("fetch active account quota (%s): %w", active.Entry.ID, errQuota)
	}

	activeQuota, errParse := quota.ParseQuotaSummary(quotaBytes)
	if errParse != nil {
		return nil, fmt.Errorf("parse active account quota (%s): %w", active.Entry.ID, errParse)
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
			Reason:        fmt.Sprintf("rotation triggered (%s), but no reserve accounts (prefix %s*) available", reason, s.cfg.ReservePrefixPrefix),
		}, nil
	}

	// 5. Fetch and evaluate quota for all reserve accounts
	type candidate struct {
		account   *AccountState
		minRemain float64
	}
	var eligible []candidate

	for _, res := range reserves {
		qBytes, err := s.client.GetQuotaSummary(ctx, res.Entry.AuthIndex, res.Entry.ProjectID)
		if err != nil {
			// Skip failing reserve, log reason
			res.QuotaErr = err
			continue
		}

		parsed, err := quota.ParseQuotaSummary(qBytes)
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
			Reason:        fmt.Sprintf("rotation triggered (%s), but all reserves exceed thresholds or failed quota check", reason),
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
			Reason: fmt.Sprintf("[DRY-RUN] Would swap %s (%s) <-> %s (%s) (reason: %s, reserve min available: %.1f%%)",
				active.Entry.ID, s.cfg.ActivePrefix, bestReserve.Entry.ID, reserveOriginalPrefix, reason, eligible[0].minRemain),
		}, nil
	}

	// 7. Execute 2-step direct swap (Policy 4A)
	// Step 1: Promote reserve account to active prefix first (transient duplicate permitted)
	nameReserve := bestReserve.Entry.Name
	if nameReserve == "" {
		nameReserve = bestReserve.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameReserve, s.cfg.ActivePrefix); err != nil {
		return nil, fmt.Errorf("failed to promote reserve account %s to active prefix %q: %w", nameReserve, s.cfg.ActivePrefix, err)
	}

	// Step 2: Demote previous active account to reserve prefix
	nameActive := active.Entry.Name
	if nameActive == "" {
		nameActive = active.Entry.ID
	}
	if err := s.client.PatchAuthPrefix(ctx, nameActive, reserveOriginalPrefix); err != nil {
		// Attempt safe recovery: restore reserve account back to its original prefix
		_ = s.client.PatchAuthPrefix(ctx, nameReserve, reserveOriginalPrefix)
		return nil, fmt.Errorf("failed to demote previous active account %s to %q: %w (rollback attempted)", nameActive, reserveOriginalPrefix, err)
	}

	return &SwitchResult{
		Rotated:         true,
		ActiveAccount:   bestReserve.Entry.ID,
		SelectedReserve: active.Entry.ID,
		Reason: fmt.Sprintf("swapped %s (%s -> %s) and %s (%s -> %s) due to: %s",
			bestReserve.Entry.ID, reserveOriginalPrefix, s.cfg.ActivePrefix,
			active.Entry.ID, s.cfg.ActivePrefix, reserveOriginalPrefix,
			reason),
	}, nil
}
