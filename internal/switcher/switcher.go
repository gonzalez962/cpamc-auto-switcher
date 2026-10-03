package switcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
	"cpamc-auto-switcher/internal/quota"
	"cpamc-auto-switcher/internal/state"
)

// AccountState represents a discovered credential and its quota metrics.
type AccountState struct {
	Entry            client.AuthFileEntry
	Prefix           string
	Profile          string
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
	state  *state.State
}

func (s *Switcher) SetState(st *state.State) { s.state = st }

func accountIdentity(f client.AuthFileEntry) string {
	if f.ID != "" {
		return f.ID
	}
	return f.Name
}

func (s *Switcher) isProblem(f client.AuthFileEntry) bool {
	return s.state != nil && s.state.HasProblem(f.Provider, accountIdentity(f))
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
func (s *Switcher) fetchAccountsConcurrently(ctx context.Context, targetProviders []string, skipDisabled bool, review bool, includeKnown bool) ([]AccountState, error) {
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
		if review && s.isProblem(f) && f.Disabled {
			s.state.MarkProblem(f.Provider, accountIdentity(f), "account is disabled; review unresolved", "disabled")
			continue
		}
		if skipDisabled && f.Disabled {
			continue
		}
		if review {
			if !s.isProblem(f) {
				continue
			}
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
				var accountErr *client.AuthFileError
				if errors.As(errMeta, &accountErr) && accountErr.StatusCode == 404 {
					mu.Lock()
					results[idx] = AccountState{Entry: f, QuotaErr: errMeta}
					mu.Unlock()
					return
				}
				mu.Lock()
				if firstFatalErr == nil {
					firstFatalErr = fmt.Errorf("read metadata for %s: %w", name, errMeta)
				}
				mu.Unlock()
				return
			}

			baseActive, _ := s.cfg.ConventionForProvider(f.Provider)
			parsed := config.ParsePrefix(meta.Prefix, baseActive)

			state := AccountState{
				Entry:            f,
				Prefix:           meta.Prefix,
				Profile:          parsed.Profile,
				ChatGPTAccountID: meta.ChatGPTAccountID,
				IsActive:         parsed.IsActive,
				IsReserve:        parsed.IsReserve,
			}

			if review {
				s.fetchQuota(ctx, &state, true)
			} else if s.isProblem(f) && !includeKnown {
				state.QuotaErr = fmt.Errorf("quota review required")
			}

			mu.Lock()
			results[idx] = state
			mu.Unlock()
		}(i)
	}

	// Wait until ALL accounts and quotas have finished loading
	wg.Wait()

	for _, account := range results {
		var managementErr *client.ManagementAPIError
		if errors.As(account.QuotaErr, &managementErr) && firstFatalErr == nil {
			firstFatalErr = managementErr
		}
	}
	if firstFatalErr == nil && s.state != nil {
		for _, account := range results {
			if account.Entry.ID == "" && account.Entry.Name == "" {
				continue
			}
			if review {
				if account.Entry.Disabled {
					s.state.MarkProblem(account.Entry.Provider, accountIdentity(account.Entry), "account is disabled; review unresolved", "disabled")
					continue
				}
				if account.QuotaErr != nil {
					reason := "account could not be reviewed; review unresolved"
					var accountErr *client.AccountQuotaError
					var authErr *client.AccountAuthError
					if errors.As(account.QuotaErr, &accountErr) || errors.As(account.QuotaErr, &authErr) {
						reason = safeQuotaProblemReason(account.QuotaErr)
					}
					s.state.MarkProblem(account.Entry.Provider, accountIdentity(account.Entry), reason, "unresolved")
					continue
				}
				if account.Quota == nil || (!account.Quota.HasFiveHour && !account.Quota.HasWeekly) {
					s.state.MarkProblem(account.Entry.Provider, accountIdentity(account.Entry), "quota response has no usable windows; review unresolved", "unresolved")
					continue
				}
				s.state.ResolveProblem(account.Entry.Provider, accountIdentity(account.Entry))
				continue
			}
			if account.QuotaErr != nil {
				var queryErr *client.QuotaQueryError
				var fileErr *client.AuthFileError
				var managementErr *client.ManagementAPIError
				var transportErr *client.TransportError
				var authErr *client.AccountAuthError
				if errors.As(account.QuotaErr, &authErr) || errors.As(account.QuotaErr, &queryErr) || (account.QuotaErr != nil && !errors.As(account.QuotaErr, &managementErr) && !errors.As(account.QuotaErr, &transportErr) && !errors.As(account.QuotaErr, &fileErr)) {
					s.state.RecordProblem(account.Entry.Provider, accountIdentity(account.Entry), safeQuotaProblemReason(account.QuotaErr))
				}
				if errors.As(account.QuotaErr, &fileErr) && fileErr.StatusCode == 404 {
					s.state.RecordProblem(account.Entry.Provider, accountIdentity(account.Entry), "account metadata unavailable; review unresolved")
				}
			}
		}
	}

	if firstFatalErr != nil {
		return nil, firstFatalErr
	}

	return results, nil
}

func safeQuotaProblemReason(err error) string {
	var accountErr *client.AccountQuotaError
	if errors.As(err, &accountErr) {
		return fmt.Sprintf("account quota request rejected by management API (%d)", accountErr.StatusCode)
	}
	var authErr *client.AccountAuthError
	if errors.As(err, &authErr) {
		return fmt.Sprintf("account authentication failed (%d)", authErr.StatusCode)
	}
	var queryErr *client.QuotaQueryError
	if errors.As(err, &queryErr) && queryErr.StatusCode != 0 {
		return fmt.Sprintf("account quota endpoint returned (%d)", queryErr.StatusCode)
	}
	return "quota query failed"
}

func (s *Switcher) recordQuotaProblem(account *AccountState) {
	if s.state == nil || account.QuotaErr == nil {
		return
	}
	var managementErr *client.ManagementAPIError
	var transportErr *client.TransportError
	if errors.As(account.QuotaErr, &managementErr) || errors.As(account.QuotaErr, &transportErr) {
		return
	}
	s.state.RecordProblem(account.Entry.Provider, accountIdentity(account.Entry), safeQuotaProblemReason(account.QuotaErr))
}

func (s *Switcher) fetchQuota(ctx context.Context, account *AccountState, includeKnown ...bool) {
	f := account.Entry
	if f.Disabled || (s.isProblem(f) && (len(includeKnown) == 0 || !includeKnown[0])) {
		return
	}
	accountOrProjectID := f.ProjectID
	if strings.EqualFold(f.Provider, "codex") && account.ChatGPTAccountID != "" {
		accountOrProjectID = account.ChatGPTAccountID
	}
	qBytes, err := s.client.GetQuotaSummaryForProvider(ctx, f.Provider, f.AuthIndex, accountOrProjectID)
	if err != nil {
		account.QuotaErr = err
		return
	}
	parsed, err := quota.ParseQuotaSummaryForProvider(f.Provider, qBytes)
	if err != nil {
		account.QuotaErr = err
		return
	}
	if parsed == nil || (!parsed.HasFiveHour && !parsed.HasWeekly) {
		account.QuotaErr = fmt.Errorf("quota response has no usable windows")
		return
	}
	account.Quota = parsed
}

// ListAccounts retrieves and evaluates all accounts for the configured provider(s) along with their quota.
func (s *Switcher) ListAccounts(ctx context.Context) ([]AccountState, error) {
	accounts, err := s.fetchAccountsConcurrently(ctx, s.cfg.ResolvedProviders(), false, false, true)
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i := range accounts {
		wg.Add(1)
		go func(i int) { defer wg.Done(); s.fetchQuota(ctx, &accounts[i], true) }(i)
	}
	wg.Wait()
	for i := range accounts {
		if accounts[i].QuotaErr != nil {
			var managementErr *client.ManagementAPIError
			if errors.As(accounts[i].QuotaErr, &managementErr) {
				return nil, managementErr
			}
			var transportErr *client.TransportError
			if errors.As(accounts[i].QuotaErr, &transportErr) {
				return nil, transportErr
			}
			var fileErr *client.AuthFileError
			if s.state != nil && !errors.As(accounts[i].QuotaErr, &transportErr) && !errors.As(accounts[i].QuotaErr, &fileErr) {
				s.state.RecordProblem(accounts[i].Entry.Provider, accountIdentity(accounts[i].Entry), safeQuotaProblemReason(accounts[i].QuotaErr))
			}
		}
	}
	if s.state != nil {
		observedByProvider := make(map[string]map[string]string)
		for i := range accounts {
			provider := accounts[i].Entry.Provider
			if observedByProvider[provider] == nil {
				observedByProvider[provider] = make(map[string]string)
			}
			if accounts[i].IsActive {
				observedByProvider[provider][accounts[i].Profile] = accountIdentity(accounts[i].Entry)
			}
		}
		for _, provider := range s.cfg.ResolvedProviders() {
			if observedByProvider[provider] == nil {
				observedByProvider[provider] = make(map[string]string)
			}
		}
		for provider, observed := range observedByProvider {
			s.state.SyncActives(provider, observed)
		}
	}

	// Sort accounts: provider alphabetically, profile alphabetically, active first, then by prefix alphabetically, then by ID
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Entry.Provider != accounts[j].Entry.Provider {
			return accounts[i].Entry.Provider < accounts[j].Entry.Provider
		}
		if accounts[i].Profile != accounts[j].Profile {
			return accounts[i].Profile < accounts[j].Profile
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
	baseActive, _ := s.cfg.ConventionForProvider(provider)
	targetParsed := config.ParsePrefix(targetItem.meta.Prefix, baseActive)
	targetProfile := targetParsed.Profile

	activePrefix, reservePrefixPrefix := s.cfg.ConventionForProfile(provider, targetProfile)

	var activeItem *fileWithMeta
	existingReserveIndices := make(map[int]bool)

	for i := range loadedFiles {
		item := &loadedFiles[i]
		if !strings.EqualFold(item.entry.Provider, provider) {
			continue
		}

		itemParsed := config.ParsePrefix(item.meta.Prefix, baseActive)
		if itemParsed.Matched && itemParsed.Profile == targetProfile {
			if itemParsed.IsActive {
				activeItem = item
			}
			if itemParsed.IsReserve {
				existingReserveIndices[itemParsed.ReserveIndex] = true
			}
		}
	}

	if s.state != nil {
		identity := ""
		if activeItem != nil {
			identity = accountIdentity(activeItem.entry)
		}
		s.state.SetActive(provider, targetProfile, identity)
	}
	if s.isProblem(targetItem.entry) {
		return nil, fmt.Errorf("problem account cannot be manually promoted; run cpamc-auto-switcher --review")
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

	if s.state != nil {
		s.state.SetActive(provider, targetProfile, accountIdentity(targetItem.entry))
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
// It discovers all distinct profiles present in the provider's accounts and evaluates each independently.
// If targetProfiles are provided, only those profiles are evaluated.
func (s *Switcher) RunProvider(ctx context.Context, provider string, dryRun bool, targetProfiles ...string) (*SwitchResult, error) {
	baseActive, _ := s.cfg.ConventionForProvider(provider)

	// Refresh metadata on every run; current prefixes, not cached identities, are authoritative.
	providerAccounts, err := s.fetchAccountsConcurrently(ctx, []string{provider}, true, false, false)
	if err != nil {
		return nil, fmt.Errorf("fetch accounts for %s: %w", provider, err)
	}

	// Cache actual active identities observed in metadata, including external changes.
	observed := make(map[string]string)
	for i := range providerAccounts {
		parsed := config.ParsePrefix(providerAccounts[i].Prefix, baseActive)
		if parsed.Matched && parsed.IsActive {
			observed[parsed.Profile] = accountIdentity(providerAccounts[i].Entry)
		}
	}
	if s.state != nil {
		s.state.SyncActives(provider, observed)
	}
	if len(providerAccounts) == 0 {
		return &SwitchResult{Rotated: false, Reason: fmt.Sprintf("no active accounts found for provider %q", provider)}, nil
	}

	// Step 2: Discover all distinct profiles present in the provider's accounts
	discoveredMap := make(map[string]bool)
	for _, acc := range providerAccounts {
		parsed := config.ParsePrefix(acc.Prefix, baseActive)
		if parsed.Matched {
			discoveredMap[parsed.Profile] = true
		}
	}

	var profilesToEval []string
	if len(targetProfiles) > 0 {
		for _, tp := range targetProfiles {
			clean := strings.TrimSpace(tp)
			if strings.EqualFold(clean, "default") {
				clean = ""
			}
			profilesToEval = append(profilesToEval, clean)
		}
	} else {
		for p := range discoveredMap {
			profilesToEval = append(profilesToEval, p)
		}
		sort.Slice(profilesToEval, func(i, j int) bool {
			if profilesToEval[i] == "" {
				return true
			}
			if profilesToEval[j] == "" {
				return false
			}
			return profilesToEval[i] < profilesToEval[j]
		})
	}

	if len(profilesToEval) == 0 {
		return &SwitchResult{
			Rotated: false,
			Reason:  fmt.Sprintf("no active or reserve accounts found for provider %q", provider),
		}, nil
	}

	type profileOutcome struct {
		profile string
		result  *SwitchResult
	}
	var outcomes []profileOutcome

	for _, profile := range profilesToEval {
		activePrefix, reservePrefixPrefix := s.cfg.ConventionForProfile(provider, profile)

		var active *AccountState
		var reserves []*AccountState

		for i := range providerAccounts {
			acc := &providerAccounts[i]
			parsed := config.ParsePrefix(acc.Prefix, baseActive)
			if !parsed.Matched || parsed.Profile != profile {
				continue
			}
			if parsed.IsActive {
				if active != nil {
					return nil, fmt.Errorf("multiple active accounts found with prefix %q for profile %q (provider %s): %s and %s",
						activePrefix, profile, provider, active.Entry.ID, acc.Entry.ID)
				}
				active = acc
			} else if parsed.IsReserve {
				reserves = append(reserves, acc)
			}
		}

		if active == nil {
			reason := fmt.Sprintf("no account currently has active prefix %q for provider %q", activePrefix, provider)
			if profile != "" {
				reason = fmt.Sprintf("no account currently has active prefix %q for profile %q (provider %q)", activePrefix, profile, provider)
			}
			outcomes = append(outcomes, profileOutcome{
				profile: profile,
				result: &SwitchResult{
					Rotated: false,
					Reason:  reason,
				},
			})
			continue
		}

		knownProblem := s.isProblem(active.Entry)
		if !knownProblem {
			s.fetchQuota(ctx, active)
		}
		if active.QuotaErr != nil {
			var managementErr *client.ManagementAPIError
			var transportErr *client.TransportError
			if errors.As(active.QuotaErr, &managementErr) || errors.As(active.QuotaErr, &transportErr) {
				return nil, fmt.Errorf("active quota check failed (%s, profile %q, %s): %w", provider, profile, active.Entry.ID, active.QuotaErr)
			}
		}
		problemActive := knownProblem || active.QuotaErr != nil || active.Quota == nil
		if problemActive && !knownProblem {
			s.recordQuotaProblem(active)
		}

		// Step 3: Evaluate threshold condition on active account
		var quotaInfoParts []string
		if !problemActive && active.Quota.HasFiveHour && active.Quota.WorstFiveHour != nil {
			quotaInfoParts = append(quotaInfoParts, fmt.Sprintf("5h %.1f%%", active.Quota.WorstFiveHour.ConsumedPercentage))
		}
		if !problemActive && active.Quota.HasWeekly && active.Quota.WorstWeekly != nil {
			quotaInfoParts = append(quotaInfoParts, fmt.Sprintf("weekly %.1f%%", active.Quota.WorstWeekly.ConsumedPercentage))
		}
		quotaSummaryStr := strings.Join(quotaInfoParts, ", ")
		if quotaSummaryStr == "" && !problemActive {
			quotaSummaryStr = fmt.Sprintf("%.1f%%", 100.0-active.Quota.MinAvailableRemaining())
		}

		shouldRotate, reason := false, ""
		if problemActive {
			shouldRotate, reason = true, "active account is quarantined or quota unavailable"
		} else {
			shouldRotate, reason = active.Quota.ShouldRotate(s.cfg.FiveHourThreshold, s.cfg.WeeklyThreshold)
		}
		if !shouldRotate {
			outcomes = append(outcomes, profileOutcome{
				profile: profile,
				result: &SwitchResult{
					Rotated:       false,
					ActiveAccount: active.Entry.ID,
					Reason:        quotaSummaryStr,
				},
			})
			continue
		}

		if len(reserves) == 0 {
			noReservesReason := fmt.Sprintf("rotation triggered (%s), but no reserve accounts (prefix %s*) available for provider %q", reason, reservePrefixPrefix, provider)
			if profile != "" {
				noReservesReason = fmt.Sprintf("rotation triggered (%s), but no reserve accounts (prefix %s*) available for profile %q (provider %q)", reason, reservePrefixPrefix, profile, provider)
			}
			outcomes = append(outcomes, profileOutcome{
				profile: profile,
				result: &SwitchResult{
					Rotated: false,
					ActiveAccount: func() string {
						if problemActive {
							return ""
						}
						return active.Entry.ID
					}(),
					Reason: noReservesReason,
				},
			})
			continue
		}

		// Step 4: Evaluate reserve candidates (all quotas are ALREADY loaded in memory)
		type candidate struct {
			account   *AccountState
			minRemain float64
		}
		var eligible []candidate

		for _, res := range reserves {
			s.fetchQuota(ctx, res)
			if res.QuotaErr != nil {
				var managementErr *client.ManagementAPIError
				var transportErr *client.TransportError
				if errors.As(res.QuotaErr, &managementErr) || errors.As(res.QuotaErr, &transportErr) {
					return nil, fmt.Errorf("reserve quota check failed (%s, profile %q, %s): %w", provider, profile, res.Entry.ID, res.QuotaErr)
				}
				s.recordQuotaProblem(res)
			}
			if res.QuotaErr != nil || res.Quota == nil || res.Entry.Disabled || s.isProblem(res.Entry) {
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
			allExceededReason := fmt.Sprintf("rotation triggered (%s), but all reserves exceed thresholds or failed quota check for provider %q", reason, provider)
			if profile != "" {
				allExceededReason = fmt.Sprintf("rotation triggered (%s), but all reserves exceed thresholds or failed quota check for profile %q (provider %q)", reason, profile, provider)
			}
			outcomes = append(outcomes, profileOutcome{
				profile: profile,
				result: &SwitchResult{
					Rotated: false,
					ActiveAccount: func() string {
						if problemActive {
							return ""
						}
						return active.Entry.ID
					}(),
					Reason: allExceededReason,
				},
			})
			continue
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

		dryRunReason := fmt.Sprintf("[DRY-RUN] Would swap %s (%s) <-> %s (%s) (reason: %s, reserve min available: %.1f%%, provider: %s)",
			active.Entry.ID, activePrefix, bestReserve.Entry.ID, reserveOriginalPrefix, reason, eligible[0].minRemain, provider)
		if profile != "" {
			dryRunReason = fmt.Sprintf("[DRY-RUN] Would swap %s (%s) <-> %s (%s) (reason: %s, reserve min available: %.1f%%, provider: %s, profile: %s)",
				active.Entry.ID, activePrefix, bestReserve.Entry.ID, reserveOriginalPrefix, reason, eligible[0].minRemain, provider, profile)
		}

		if dryRun {
			outcomes = append(outcomes, profileOutcome{
				profile: profile,
				result: &SwitchResult{
					Rotated:         true,
					ActiveAccount:   active.Entry.ID,
					SelectedReserve: bestReserve.Entry.ID,
					Reason:          dryRunReason,
				},
			})
			continue
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

		if s.state != nil {
			s.state.SetActive(provider, profile, accountIdentity(bestReserve.Entry))
		}
		swapReason := fmt.Sprintf("swapped %s (%s -> %s) and %s (%s -> %s) due to: %s (provider: %s)",
			bestReserve.Entry.ID, reserveOriginalPrefix, activePrefix,
			active.Entry.ID, activePrefix, reserveOriginalPrefix,
			reason, provider)
		if profile != "" {
			swapReason = fmt.Sprintf("swapped %s (%s -> %s) and %s (%s -> %s) due to: %s (provider: %s, profile: %s)",
				bestReserve.Entry.ID, reserveOriginalPrefix, activePrefix,
				active.Entry.ID, activePrefix, reserveOriginalPrefix,
				reason, provider, profile)
		}

		outcomes = append(outcomes, profileOutcome{
			profile: profile,
			result: &SwitchResult{
				Rotated:         true,
				ActiveAccount:   bestReserve.Entry.ID,
				SelectedReserve: active.Entry.ID,
				Reason:          swapReason,
			},
		})
	}

	if len(outcomes) == 1 {
		return outcomes[0].result, nil
	}

	var anyRotated bool
	var reasons []string
	var lastActiveAccount string
	var lastSelectedReserve string

	for _, oc := range outcomes {
		profLabel := oc.profile
		if profLabel == "" {
			profLabel = "default"
		}
		if oc.result.Rotated {
			anyRotated = true
			lastActiveAccount = oc.result.ActiveAccount
			lastSelectedReserve = oc.result.SelectedReserve
		}
		reasons = append(reasons, fmt.Sprintf("[%s] %s", profLabel, oc.result.Reason))
	}

	return &SwitchResult{
		Rotated:         anyRotated,
		ActiveAccount:   lastActiveAccount,
		SelectedReserve: lastSelectedReserve,
		Reason:          strings.Join(reasons, " | "),
	}, nil
}

// Review retries every recorded account without applying filters or rotating prefixes.
func (s *Switcher) Review(ctx context.Context) ([]state.ProblemAccount, error) {
	if s.state == nil {
		return nil, fmt.Errorf("review state unavailable")
	}
	problems := s.state.ProblemSnapshot()
	providers := make([]string, 0)
	seen := make(map[string]bool)
	for _, p := range problems {
		k := strings.ToLower(p.Provider)
		if !seen[k] {
			providers = append(providers, p.Provider)
			seen[k] = true
		}
	}
	files, err := s.client.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth files for review: %w", err)
	}
	found := make(map[string]bool)
	for _, f := range files {
		if s.state.HasProblem(f.Provider, accountIdentity(f)) {
			found[strings.ToLower(f.Provider)+"\x00"+accountIdentity(f)] = true
		}
	}
	for _, p := range problems {
		key := strings.ToLower(p.Provider) + "\x00" + p.Identity
		if !found[key] {
			s.state.MarkProblem(p.Provider, p.Identity, "account is missing; review unresolved", "missing")
		}
	}
	if _, err := s.fetchAccountsConcurrently(ctx, providers, false, true, false); err != nil {
		return nil, err
	}
	return s.state.ProblemSnapshot(), nil
}

// Run executes the evaluation and switching workflow across all resolved providers asynchronously.
func (s *Switcher) Run(ctx context.Context, dryRun bool, targetProfiles ...string) (*SwitchResult, error) {
	providers := s.cfg.ResolvedProviders()
	if len(providers) == 1 {
		res, err := s.RunProvider(ctx, providers[0], dryRun, targetProfiles...)
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
			res, err := s.RunProvider(ctx, prov, dryRun, targetProfiles...)
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
		}
		reasons = append(reasons, fmt.Sprintf("%s: %s", p, outcome.result.Reason))
	}

	return &SwitchResult{
		Rotated:         anyRotated,
		ActiveAccount:   lastActiveAccount,
		SelectedReserve: lastSelectedReserve,
		Reason:          strings.Join(reasons, " | "),
	}, nil
}
