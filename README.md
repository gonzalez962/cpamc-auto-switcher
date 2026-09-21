# cpamc-auto-switcher

Standalone Go utility to automatically inspect account quotas on CLIProxyAPI and rotate active account prefixes with reserve accounts when rate limits are approached, with manual switch capabilities. Supports both **Antigravity** and **Codex** providers.

## Features & Rotation Rules

- **Providers Supported**:
  - `antigravity`: Active `agy`, Reserves `agy_*` (e.g. `agy_1`, `agy_2`).
  - `codex`: Active `codex`, Reserves `codex_*` (e.g. `codex_1`, `codex_2`).
  - By default, evaluates `all` configured providers.
- **Hierarchical Profile-Based Pools**:
  - Dynamic automatic detection of profiles from prefixes for both `antigravity` and `codex`.
  - **Default / Root Pool** (empty profile):
    - Active prefix: `agy` or `codex`
    - Reserve prefixes: `agy_<index>` (e.g. `agy_1`, `agy_2`) or `codex_<index>` (e.g. `codex_1`)
  - **Profile Pool P** (e.g. `p1`, `team_a`):
    - Active prefix: `<provider>_<profile>` (e.g. `agy_p1`, `codex_p1`, `agy_team_a`)
    - Reserve prefixes: `<provider>_<profile>_<index>` (e.g. `agy_p1_1`, `agy_p1_2`, `codex_p1_1`)
  - **Profile Isolation**: Each profile pool evaluates and rotates independently. When an active account in profile `p1` reaches quota limits, it is swapped with the best reserve within `p1`, leaving the default pool and all other profiles completely unaffected.
- **Rotation Triggers**:
  - 5-Hour limit consumed $\ge 90\%$, **OR**
  - Weekly limit consumed $\ge 95\%$.
- **Window Handling (Policy 1A)**:
  - If an upstream provider returns only the `Weekly` window (or single window), the 5-hour requirement is gracefully omitted and only the available window is evaluated.
- **Multi-Group Aggregation (Policy 2A)**:
  - When quota responses contain multiple model groups, the worst-case limit (lowest remaining capacity) across all groups is evaluated.
- **Reserve Account Selection (Policy 3B)**:
  - The reserve account with the highest minimum remaining quota across its available windows is selected as the replacement.
- **Direct Prefix Swap (Policy 4A)**:
  - Swaps prefixes directly (`active` $\leftrightarrow$ candidate reserve prefix) promoting the reserve first to avoid request routing downtime.
- **Credential Storage**:
  - Credentials and endpoint are stored under `~/.local/share/cpamc-auto-switcher/config.json` with strict directory (`0700`) and file (`0600`) permissions.

---

## Installation & Setup

### 1. Build & Install Binary

```bash
cd cpamc-auto-switcher
go install ./cmd/cpamc-auto-switcher
```

### 2. Configure Credentials

Run interactive setup to configure the endpoint URL, management key, and quota thresholds:

```bash
cpamc-auto-switcher -init
```

The interactive wizard supports both initial setup and updating existing configurations:
- **Endpoint URL**: Displays the current endpoint (or `http://localhost:8000` by default). Pressing Enter retains the current endpoint.
- **Management Key**: If a key is already saved, displays `[leave blank to keep current]`; pressing Enter keeps the existing key. On first-time setup, a non-empty key is required.
- **5-Hour Threshold**: Displays the current or default percentage (default `90.0`). Pressing Enter keeps the default, or enter a custom float.
- **Weekly Threshold**: Displays the current or default percentage (default `95.0`). Pressing Enter keeps the default, or enter a custom float.
- **Preserved Settings**: Existing configuration settings including `provider`, `active_prefix`, `reserve_prefix_prefix`, and `cooldown_minutes` are safely preserved when updating.

Default config file location:
`~/.local/share/cpamc-auto-switcher/config.json` (or supply custom path via `-config <path>`)

Example configuration:
```json
{
  "endpoint": "http://localhost:8000",
  "management_key": "YOUR_MANAGEMENT_KEY",
  "provider": "antigravity",
  "active_prefix": "agy",
  "reserve_prefix_prefix": "agy_",
  "five_hour_threshold": 90.0,
  "weekly_threshold": 95.0
}
```

---

## Usage

### List Accounts and Current Usage
List all configured accounts for all providers (or a specific provider and profile), their current prefix status, and consumption metrics (includes a `PROFILE` column):

```bash
# List all providers (antigravity and codex) and all profiles:
cpamc-auto-switcher -list

# Filter by provider:
cpamc-auto-switcher -list -provider codex
cpamc-auto-switcher -list -provider antigravity

# Filter by profile (e.g. p1 or default root pool):
cpamc-auto-switcher -list -profile p1
cpamc-auto-switcher -list -profile default
```

### Manually Set Active Account
Manually promote a specific account (by prefix, ID, filename, or email) to be the active account for its provider and profile pool. The previous active account for that pool is safely demoted to a reserve prefix:

```bash
# Switch Antigravity default pool:
cpamc-auto-switcher -switch "agy_1"

# Switch Antigravity profile p1:
cpamc-auto-switcher -switch "agy_p1_1"

# Switch Codex profile dev:
cpamc-auto-switcher -switch "codex_dev_1"

# Switch Codex default pool:
cpamc-auto-switcher -switch "codex_1"

# By email or filename:
cpamc-auto-switcher -switch "user@example.com.json"
# Or simulate with dry-run:
cpamc-auto-switcher -switch "agy_p1_2" -dry-run
```

### Check Quota / Dry-Run (Automatic Rotation Simulation)
Check quotas and simulate whether an automatic rotation would take place without altering account prefixes:

```bash
cpamc-auto-switcher -check
# or filter by provider and profile:
cpamc-auto-switcher -check -provider antigravity -profile p1
```

### Perform Automatic Rotation
Inspect accounts, fetch live quotas, and perform a prefix swap if limits are exceeded. Evaluates each profile pool independently. A 5-minute cooldown is enforced between quota checks by default:

```bash
# Evaluate and rotate all providers across all discovered profiles:
cpamc-auto-switcher

# Target a specific provider:
cpamc-auto-switcher -provider codex

# Target a specific profile within provider(s):
cpamc-auto-switcher -profile p1
```

### Force Immediate Check (Bypass Cooldown)
Bypass the cooldown period and query limits immediately:

```bash
cpamc-auto-switcher -force
```

### Custom Cooldown Duration
Specify a custom cooldown interval (e.g. 1m, 10m):

```bash
cpamc-auto-switcher -cooldown 10m
```

### Verbose Mode
Display detailed evaluation logs (including remaining cooldown time if active):

```bash
cpamc-auto-switcher -verbose
```

### Quota Threshold Flags
Override the quota consumption trigger thresholds via CLI flags for the current run without modifying `config.json`:

- `-five-hour-threshold <float>`: Override the 5-hour consumption threshold percentage.
- `-5h-threshold <float>`: Short alias for `-five-hour-threshold`.
- `-weekly-threshold <float>`: Override the weekly consumption threshold percentage.

```bash
# Override 5-hour threshold (e.g. rotate when >= 80% consumed):
cpamc-auto-switcher -five-hour-threshold 80.0

# Using the short alias for 5-hour threshold:
cpamc-auto-switcher -5h-threshold 80.0

# Override weekly threshold (e.g. rotate when >= 90% consumed):
cpamc-auto-switcher -weekly-threshold 90.0

# Combine threshold overrides with dry-run check or automatic rotation:
cpamc-auto-switcher -check -5h-threshold 85.0 -weekly-threshold 92.0
cpamc-auto-switcher -profile p1 -5h-threshold 80.0 -weekly-threshold 90.0
```
