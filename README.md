# cpamc-auto-switcher

Standalone Go utility to automatically inspect Antigravity account quotas on CLIProxyAPI and rotate the active account prefix (`agy`) with reserve accounts (`agy_*`) when rate limits are approached, with manual switch capabilities.

## Features & Rotation Rules

- **Provider**: Antigravity (`antigravity`).
- **Account Prefix Conventions**:
  - Active account: `prefix: "agy"`
  - Reserve accounts: `prefix: "agy_*"` (e.g. `agy_1`, `agy_2`).
- **Rotation Triggers**:
  - 5-Hour limit consumed $\ge 90\%$, **OR**
  - Weekly limit consumed $\ge 95\%$.
- **Window Handling (Policy 1A)**:
  - If upstream Antigravity returns only the `Weekly` window (standard Google Cloud Code behavior), the 5-hour requirement is gracefully omitted and only the weekly limit is evaluated.
- **Multi-Group Aggregation (Policy 2A)**:
  - When quota responses contain multiple model groups, the worst-case limit (lowest remaining capacity) across all groups is evaluated.
- **Reserve Account Selection (Policy 3B)**:
  - The reserve account with the highest minimum remaining quota across its available windows is selected as the replacement.
- **Direct Prefix Swap (Policy 4A)**:
  - Swaps prefixes directly (`agy` $\leftrightarrow$ candidate reserve prefix) promoting the reserve first to avoid request routing downtime.
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

Run interactive setup to configure the endpoint URL and management key:

```bash
cpamc-auto-switcher -init
```

Default config file location:
`~/.local/share/cpamc-auto-switcher/config.json`

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
List all configured accounts for the provider, their current prefix status, and consumption metrics:

```bash
cpamc-auto-switcher -list
```

### Manually Set Active Account
Manually promote a specific account (by prefix, ID, filename, or email) to be the active account (`agy`). The previous active account is safely demoted to a reserve prefix:

```bash
# By prefix:
cpamc-auto-switcher -switch "agy_1"

# By email or filename:
cpamc-auto-switcher -switch "antigravity-user@example.com.json"
# Or simulate with dry-run:
cpamc-auto-switcher -switch "agy_1" -dry-run
```

### Check Quota / Dry-Run (Automatic Rotation Simulation)
Check quotas and simulate whether an automatic rotation would take place without altering account prefixes:

```bash
cpamc-auto-switcher -check
# or
cpamc-auto-switcher -dry-run
```

### Perform Automatic Rotation
Inspect accounts, fetch live quotas, and perform a prefix swap if limits are exceeded. A 5-minute cooldown is enforced between quota checks by default to prevent repetitive CLI queries. If invoked within the cooldown window, the command outputs nothing and exits with 0:

```bash
cpamc-auto-switcher
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
