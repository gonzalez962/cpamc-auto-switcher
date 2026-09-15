package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
	"cpamc-auto-switcher/internal/state"
	"cpamc-auto-switcher/internal/switcher"
)

func main() {
	configPathFlag := flag.String("config", "", "Path to configuration file (default: ~/.local/share/cpamc-auto-switcher/config.json)")
	initFlag := flag.Bool("init", false, "Initialize or update credentials configuration interactively")
	listFlag := flag.Bool("list", false, "List all accounts, their prefix, and current quota usage")
	switchFlag := flag.String("switch", "", "Manually promote specified account (by prefix, ID, filename, or email) to active (agy)")
	checkFlag := flag.Bool("check", false, "Check quotas and evaluate rotation without modifying prefixes (dry-run)")
	dryRunFlag := flag.Bool("dry-run", false, "Alias for --check")
	endpointFlag := flag.String("endpoint", "", "Override API endpoint (e.g. http://localhost:8000)")
	keyFlag := flag.String("key", "", "Override management key")
	verboseFlag := flag.Bool("verbose", false, "Enable detailed logging")
	forceFlag := flag.Bool("force", false, "Bypass cooldown timeout and force quota evaluation immediately")
	cooldownFlag := flag.Duration("cooldown", 0, "Override cooldown duration between quota checks (e.g. 5m; default: 5m from config)")

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
	if strings.TrimSpace(*endpointFlag) != "" {
		cfg.Endpoint = strings.TrimSpace(*endpointFlag)
	}
	if strings.TrimSpace(*keyFlag) != "" {
		cfg.ManagementKey = strings.TrimSpace(*keyFlag)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation error: %v\n", err)
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Printf("[INFO] Using endpoint: %s\n", cfg.Endpoint)
		fmt.Printf("[INFO] Target provider: %s\n", cfg.Provider)
		fmt.Printf("[INFO] Active prefix: %s | Reserve prefix: %s*\n", cfg.ActivePrefix, cfg.ReservePrefixPrefix)
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

		if len(accounts) == 0 {
			fmt.Printf("No accounts found for provider %q\n", cfg.Provider)
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "STATUS\tACCOUNT ID\tPREFIX\t5H CONSUMED\tWEEKLY CONSUMED\tMIN AVAILABLE\tDETAILS")

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

			fiveHStr := "N/A"
			weeklyStr := "N/A"
			minAvailStr := "N/A"
			detailsStr := "-"

			if acc.QuotaErr != nil {
				detailsStr = fmt.Sprintf("Quota error: %v", acc.QuotaErr)
			} else if acc.Quota != nil {
				if acc.Quota.HasFiveHour && acc.Quota.WorstFiveHour != nil {
					fiveHStr = fmt.Sprintf("%.1f%%", acc.Quota.WorstFiveHour.ConsumedPercentage)
				}
				if acc.Quota.HasWeekly && acc.Quota.WorstWeekly != nil {
					weeklyStr = fmt.Sprintf("%.1f%%", acc.Quota.WorstWeekly.ConsumedPercentage)
				}
				minAvailStr = fmt.Sprintf("%.1f%%", acc.Quota.MinAvailableRemaining())
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				statusTag,
				acc.Entry.ID,
				acc.Prefix,
				fiveHStr,
				weeklyStr,
				minAvailStr,
				detailsStr,
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

	res, err := sw.Run(ctx, isDryRun)
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
		fmt.Printf("[OK] %s\n", res.Reason)
	}
}

func runInteractiveInit(customPath string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=== cpamc-auto-switcher configuration setup ===")

	fmt.Print("Enter CLIProxyAPI endpoint URL (e.g. http://localhost:8000): ")
	endpoint, _ := reader.ReadString('\n')
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "http://localhost:8000"
	}

	fmt.Print("Enter management key: ")
	key, _ := reader.ReadString('\n')
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("management key cannot be empty")
	}

	cfg := config.NewDefaultConfig()
	cfg.Endpoint = endpoint
	cfg.ManagementKey = key

	path, err := cfg.Save(customPath)
	if err != nil {
		return err
	}

	fmt.Printf("\nConfiguration successfully saved to %s (permissions 0600)\n", path)
	return nil
}
