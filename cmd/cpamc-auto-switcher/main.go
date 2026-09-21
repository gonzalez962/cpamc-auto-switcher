package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
	"cpamc-auto-switcher/internal/state"
	"cpamc-auto-switcher/internal/switcher"
)

var (
	emailRegex  = regexp.MustCompile(`[a-zA-Z0-9._%+]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	suffixStrip = regexp.MustCompile(`(?i)(?:-plus)?\.json$`)
)

func extractAccountEmail(id, name, email string) string {
	if email != "" && emailRegex.MatchString(email) {
		cleanEmail := suffixStrip.ReplaceAllString(email, "")
		if match := emailRegex.FindString(cleanEmail); match != "" {
			return match
		}
	}
	if id != "" {
		cleanID := suffixStrip.ReplaceAllString(id, "")
		if match := emailRegex.FindString(cleanID); match != "" {
			return match
		}
	}
	if name != "" {
		cleanName := suffixStrip.ReplaceAllString(name, "")
		if match := emailRegex.FindString(cleanName); match != "" {
			return match
		}
	}
	if strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return strings.TrimSpace(name)
}

func abbreviateEmail(email string) string {
	atIdx := strings.LastIndex(email, "@")
	if atIdx == -1 {
		return email
	}
	local := email[:atIdx]
	domain := email[atIdx:]
	runes := []rune(local)
	if len(runes) <= 6 {
		return email
	}
	return string(runes[:3]) + "..." + string(runes[len(runes)-3:]) + domain
}

func main() {
	configPathFlag := flag.String("config", "", "Path to configuration file (default: ~/.local/share/cpamc-auto-switcher/config.json)")
	initFlag := flag.Bool("init", false, "Initialize or update credentials configuration interactively")
	listFlag := flag.Bool("list", false, "List all accounts, their prefix, and current quota usage")
	providerFlag := flag.String("provider", "", "Target provider to evaluate (antigravity, codex, or all; default from config or 'all')")
	profileFlag := flag.String("profile", "", "Filter accounts or evaluation by profile (e.g. 'p1', or 'default' for unprofiled)")
	switchFlag := flag.String("switch", "", "Manually promote specified account (by prefix, ID, filename, or email) to active")
	checkFlag := flag.Bool("check", false, "Check quotas and evaluate rotation without modifying prefixes (dry-run)")
	dryRunFlag := flag.Bool("dry-run", false, "Alias for --check")
	endpointFlag := flag.String("endpoint", "", "Override API endpoint (e.g. http://localhost:8000)")
	keyFlag := flag.String("key", "", "Override management key")
	verboseFlag := flag.Bool("verbose", false, "Enable detailed logging")
	forceFlag := flag.Bool("force", false, "Bypass cooldown timeout and force quota evaluation immediately")
	cooldownFlag := flag.Duration("cooldown", 0, "Override cooldown duration between quota checks (e.g. 5m; default: 5m from config)")
	fiveHourThresholdFlag, fiveHThresholdFlag, weeklyThresholdFlag := addThresholdFlags(flag.CommandLine)

	flag.Parse()

	// 1. Interactive configuration setup if requested
	if *initFlag {
		if err := runInteractiveInit(*configPathFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 2. Load configuration
	cfg, loadedPath, err := config.Load(*configPathFlag)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Config file not found at %s. Run with --init to configure, or supply --config <path>.\n", loadedPath)
		} else {
			fmt.Fprintf(os.Stderr, "Error loading configuration from %s: %v\n", loadedPath, err)
		}
		os.Exit(1)
	}

	// Apply CLI overrides if provided
	if strings.TrimSpace(*providerFlag) != "" {
		cfg.Provider = strings.TrimSpace(*providerFlag)
	}
	if strings.TrimSpace(*endpointFlag) != "" {
		cfg.Endpoint = strings.TrimSpace(*endpointFlag)
	}
	if strings.TrimSpace(*keyFlag) != "" {
		cfg.ManagementKey = strings.TrimSpace(*keyFlag)
	}

	if err := applyThresholdOverrides(cfg, *fiveHourThresholdFlag, *fiveHThresholdFlag, *weeklyThresholdFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation error: %v\n", err)
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Printf("[INFO] Using endpoint: %s\n", cfg.Endpoint)
		fmt.Printf("[INFO] Target provider(s): %v\n", cfg.ResolvedProviders())
		if strings.TrimSpace(*profileFlag) != "" {
			fmt.Printf("[INFO] Target profile: %s\n", strings.TrimSpace(*profileFlag))
		}
		fmt.Printf("[INFO] Thresholds: 5-Hour >= %.1f%% | Weekly >= %.1f%%\n", cfg.FiveHourThreshold, cfg.WeeklyThreshold)
	}

	// 3. Instantiate client and switcher
	cli, err := client.New(cfg.Endpoint, cfg.ManagementKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize API client: %v\n", err)
		os.Exit(1)
	}

	sw := switcher.New(cfg, cli)

	// Load runtime state (tracks last check time for cooldown)
	statePath, errStatePath := state.DefaultStatePath(loadedPath)
	var appState *state.State
	if errStatePath == nil {
		var errLoadState error
		appState, errLoadState = state.Load(statePath)
		if errLoadState != nil && *verboseFlag {
			fmt.Fprintf(os.Stderr, "[WARN] Could not load state from %s: %v\n", statePath, errLoadState)
		}
	} else {
		appState = &state.State{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	isDryRun := *checkFlag || *dryRunFlag

	// 4. Handle --switch manual promotion command
	if strings.TrimSpace(*switchFlag) != "" {
		res, err := sw.SwitchToAccount(ctx, *switchFlag, isDryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Manual switch failed: %v\n", err)
			os.Exit(1)
		}

		if isDryRun {
			fmt.Printf("[DRY-RUN] %s\n", res.Reason)
		} else {
			fmt.Printf("[SWITCHED] %s\n", res.Reason)
		}
		return
	}

	// 5. Handle --list command
	if *listFlag {
		accounts, err := sw.ListAccounts(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list accounts: %v\n", err)
			os.Exit(1)
		}

		if strings.TrimSpace(*profileFlag) != "" {
			targetProf := strings.TrimSpace(*profileFlag)
			var filtered []switcher.AccountState
			for _, acc := range accounts {
				match := false
				if strings.EqualFold(targetProf, "default") {
					match = (acc.Profile == "" || strings.EqualFold(acc.Profile, "default"))
				} else {
					match = strings.EqualFold(acc.Profile, targetProf)
				}
				if match {
					filtered = append(filtered, acc)
				}
			}
			accounts = filtered
		}

		if len(accounts) == 0 {
			if strings.TrimSpace(*profileFlag) != "" {
				fmt.Printf("No accounts found for provider(s) %v with profile %q\n", cfg.ResolvedProviders(), *profileFlag)
			} else {
				fmt.Printf("No accounts found for provider(s) %v\n", cfg.ResolvedProviders())
			}
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "STATUS\tPROVIDER\tPROFILE\tACCOUNT ID\tPREFIX\t5H CONSUMED\tWEEKLY CONSUMED\tMIN AVAILABLE")

		for _, acc := range accounts {
			statusTag := "[RESERVE]"
			if acc.IsActive {
				statusTag = "[ACTIVE]"
			} else if !acc.IsReserve {
				statusTag = "[OTHER]"
			}
			if acc.Entry.Disabled {
				statusTag = "[DISABLED]"
			}

			profileDisplay := acc.Profile
			if profileDisplay == "" {
				profileDisplay = "default"
			}

			fiveHStr := "N/A"
			weeklyStr := "N/A"
			minAvailStr := "N/A"

			if acc.QuotaErr != nil {
				fiveHStr = "ERR"
				weeklyStr = "ERR"
				minAvailStr = "ERR"
			} else if acc.Quota != nil {
				if acc.Quota.HasFiveHour && acc.Quota.WorstFiveHour != nil {
					fiveHStr = fmt.Sprintf("%.1f%%", acc.Quota.WorstFiveHour.ConsumedPercentage)
				}
				if acc.Quota.HasWeekly && acc.Quota.WorstWeekly != nil {
					weeklyStr = fmt.Sprintf("%.1f%%", acc.Quota.WorstWeekly.ConsumedPercentage)
				}
				minAvailStr = fmt.Sprintf("%.1f%%", acc.Quota.MinAvailableRemaining())
			}

			accountDisplay := abbreviateEmail(extractAccountEmail(acc.Entry.ID, acc.Entry.Name, acc.Entry.Email))

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				statusTag,
				acc.Entry.Provider,
				profileDisplay,
				accountDisplay,
				acc.Prefix,
				fiveHStr,
				weeklyStr,
				minAvailStr,
			)
		}
		_ = w.Flush()

		// Update last check timestamp since quotas were queried
		if errStatePath == nil {
			appState.LastCheck = time.Now().UTC()
			_ = appState.Save(statePath)
		}
		return
	}

	// 6. Handle automatic rotation run / check with cooldown timeout
	cooldown := time.Duration(cfg.CooldownMinutes * float64(time.Minute))
	if *cooldownFlag > 0 {
		cooldown = *cooldownFlag
	}

	// If within cooldown timeout and not forced, return nothing and exit cleanly
	if !*forceFlag && !appState.ShouldCheck(cooldown, time.Now()) {
		if *verboseFlag {
			fmt.Printf("[INFO] Cooldown active (last check was %s ago, cooldown is %v); skipping quota check\n",
				time.Since(appState.LastCheck).Round(time.Second), cooldown)
		}
		return
	}

	var res *switcher.SwitchResult
	if strings.TrimSpace(*profileFlag) != "" {
		res, err = sw.Run(ctx, isDryRun, strings.TrimSpace(*profileFlag))
	} else {
		res, err = sw.Run(ctx, isDryRun)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Switcher execution failed: %v\n", err)
		os.Exit(1)
	}

	// Update last check timestamp on successful check
	if errStatePath == nil {
		appState.LastCheck = time.Now().UTC()
		if errSave := appState.Save(statePath); errSave != nil && *verboseFlag {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to save state to %s: %v\n", statePath, errSave)
		}
	}

	if res.Rotated {
		if isDryRun {
			fmt.Printf("[DRY-RUN] %s\n", res.Reason)
		} else {
			fmt.Printf("[ROTATED] %s\n", res.Reason)
		}
	} else {
		fmt.Println(res.Reason)
	}
}

func addThresholdFlags(fs *flag.FlagSet) (fiveHour *float64, fiveHAlias *float64, weekly *float64) {
	fiveHour = fs.Float64("five-hour-threshold", 0, "Override 5-hour quota consumption threshold percentage (e.g. 85.0)")
	fiveHAlias = fs.Float64("5h-threshold", 0, "Alias for -five-hour-threshold")
	weekly = fs.Float64("weekly-threshold", 0, "Override weekly quota consumption threshold percentage (e.g. 90.0)")
	return
}

func applyThresholdOverrides(cfg *config.Config, fiveHour, fiveHAlias, weekly float64) error {
	if fiveHour > 0 {
		cfg.FiveHourThreshold = fiveHour
	} else if fiveHAlias > 0 {
		cfg.FiveHourThreshold = fiveHAlias
	}
	if weekly > 0 {
		cfg.WeeklyThreshold = weekly
	}
	return cfg.Validate()
}

func readInputLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runInteractiveInit(customPath string) error {
	return runInteractiveInitWithIO(customPath, os.Stdin, os.Stdout)
}

func runInteractiveInitWithIO(customPath string, r io.Reader, w io.Writer) error {
	reader := bufio.NewReader(r)

	cfg := config.NewDefaultConfig()
	targetPath := customPath
	if strings.TrimSpace(targetPath) == "" {
		if defPath, err := config.DefaultConfigPath(); err == nil {
			targetPath = defPath
		}
	}

	if targetPath != "" {
		if data, err := os.ReadFile(targetPath); err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				return fmt.Errorf("failed to parse existing config at %s: %w", targetPath, err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read existing config at %s: %w", targetPath, err)
		}
	}

	fmt.Fprintln(w, "=== cpamc-auto-switcher configuration setup ===")

	// 1. Endpoint
	defaultEndpoint := "http://localhost:8000"
	if strings.TrimSpace(cfg.Endpoint) != "" {
		defaultEndpoint = strings.TrimSpace(cfg.Endpoint)
	}
	fmt.Fprintf(w, "Enter CLIProxyAPI endpoint URL [%s]: ", defaultEndpoint)
	endpointInput, err := readInputLine(reader)
	if err != nil {
		return err
	}
	if endpointInput != "" {
		cfg.Endpoint = endpointInput
	} else {
		cfg.Endpoint = defaultEndpoint
	}

	// 2. Management Key
	if strings.TrimSpace(cfg.ManagementKey) != "" {
		fmt.Fprint(w, "Enter management key [leave blank to keep current]: ")
		keyInput, err := readInputLine(reader)
		if err != nil {
			return err
		}
		if keyInput != "" {
			cfg.ManagementKey = keyInput
		}
	} else {
		fmt.Fprint(w, "Enter management key: ")
		keyInput, err := readInputLine(reader)
		if err != nil {
			return err
		}
		if keyInput == "" {
			return fmt.Errorf("management key cannot be empty")
		}
		cfg.ManagementKey = keyInput
	}

	// 3. 5-Hour Threshold
	default5H := 90.0
	if cfg.FiveHourThreshold > 0 {
		default5H = cfg.FiveHourThreshold
	}
	fmt.Fprintf(w, "Enter 5-hour threshold percentage [%.1f]: ", default5H)
	fiveHInput, err := readInputLine(reader)
	if err != nil {
		return err
	}
	if fiveHInput != "" {
		val, err := strconv.ParseFloat(fiveHInput, 64)
		if err != nil {
			return fmt.Errorf("invalid 5-hour threshold: %w", err)
		}
		cfg.FiveHourThreshold = val
	} else {
		cfg.FiveHourThreshold = default5H
	}

	// 4. Weekly Threshold
	defaultWeekly := 95.0
	if cfg.WeeklyThreshold > 0 {
		defaultWeekly = cfg.WeeklyThreshold
	}
	fmt.Fprintf(w, "Enter weekly threshold percentage [%.1f]: ", defaultWeekly)
	weeklyInput, err := readInputLine(reader)
	if err != nil {
		return err
	}
	if weeklyInput != "" {
		val, err := strconv.ParseFloat(weeklyInput, 64)
		if err != nil {
			return fmt.Errorf("invalid weekly threshold: %w", err)
		}
		cfg.WeeklyThreshold = val
	} else {
		cfg.WeeklyThreshold = defaultWeekly
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration validation error: %w", err)
	}

	path, err := cfg.Save(customPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "\nConfiguration successfully saved to %s (permissions 0600)\n", path)
	return nil
}
