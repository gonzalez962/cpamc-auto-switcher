# Visual Profile Plugin (Linux amd64 & arm64) Release Guide

This document defines the release architecture, access control model, verification pipeline, and operational procedures for publishing pre-compiled Linux amd64 and Linux arm64 (glibc) shared library binaries (`.so`) of the `visual-profile` plugin.

---

## 1. Overview & Architecture

Releases are published strictly through manual dispatch via the GitHub Actions workflow:
`.github/workflows/visual-profile-release.yml`

### Execution Model: Manual Dispatch Only
- **Trigger**: The workflow is triggered exclusively via `workflow_dispatch` from the `main` branch (`refs/heads/main`).
- **No Push Triggers**: Pushing commits or tags to the repository does **not** trigger any release workflow.
- **Workflow Presence on `main`**: On GitHub Actions, `workflow_dispatch` workflows are only available and runnable once the workflow definition file (`.github/workflows/visual-profile-release.yml`) is committed and merged into `main`.
- **New Release Tag Input**: The operator enters a **new** release version tag matching `vX.Y.Z` (e.g. `v0.1.0`) directly into the workflow dispatch input.
- **No Local Tag Push Required**: Operators do **not** create or push any git tag beforehand (no local tag push required). Git tags are created directly by GitHub during release creation at the tested commit.

### Core Security & Provenance Principles
- **No Hardcoded Actors**: Execution rights are governed entirely by GitHub repository collaborator permissions, not static usernames in YAML.
- **Repository Boundary Isolation**: Asserts `github.repository == 'gonzalez962/cpamc-auto-switcher'`, preventing accidental execution or asset pollution on forks.
- **Strict Version Tag Format**: Tag inputs must strictly conform to regex `^v[0-9]+\.[0-9]+\.[0-9]+$` before any git fetch or checkout operations proceed.
- **Checked-Out SHA Pinned to `main` Before Mutation**:
  The workflow verifies that the checked-out commit matches the dispatched `github.sha` and confirms that this commit is an ancestor of `origin/main` (`git merge-base --is-ancestor`) *before* running builds, tests, or artifact generation.
- **Fail-Closed Remote Tag and Release Collision Checks**:
  Both `build` and `publish` stages check that neither the git tag (via `git ls-remote --exit-code`) nor the GitHub release (via `gh api repos/${REPO}/releases/tags/${TAG}`) exists on remote origin. The check is strictly fail-closed: execution proceeds only when the tag check returns exit code 2 (not found) and the release API returns HTTP 404. Any collision (HTTP 200 / exit 0), auth failure, rate limit, or transport error aborts the workflow immediately.
- **Multi-Component Version Parity**:
  The workflow validates that the release tag strictly matches:
  - Go plugin version: `plugins/visual-profile/internal/version/version.go` (`const Version = "X.Y.Z"`)
  - Web package version: `plugins/visual-profile/web/package.json` (`"version": "X.Y.Z"`)
- **Embedded Web Asset Parity Check**:
  Frontend assets are built with `npm run build` from lockfile (`npm ci`) and compared against tracked assets via `git diff --exit-code -- plugins/visual-profile/internal/web/assets/index.html` to guarantee that the single-file UI embedded in the Go binary matches the committed source.
- **Automated Validation**:
  Full test suites run before compilation: frontend unit tests (`npm test -- --run`) and Go package tests (`go test -v ./...`).
- **Least-Privilege Token Permissions**:
  - The workflow default and `build` job use read-only tokens (`permissions: contents: read`).
  - Write access (`permissions: contents: write`) is granted strictly to the downstream `publish` job.
- **Release and Tag Creation Without `--verify-tag`**:
  At publish time, `gh release create` is invoked targeting the tested commit SHA (`--target "${TARGET_COMMIT}"`) **without** `--verify-tag` and without `--draft`. This produces an immediately published GitHub release and creates the new git tag at the verified, tested commit SHA. Release creation and asset uploads are separate sequential operations (not an atomic tag+asset delivery): GitHub creates the published release record and git tag first, then GitHub CLI uploads attached binary assets.
- **Safe Release Notes Generation**:
  Release notes are emitted using explicit POSIX `printf` commands, avoiding bash heredoc delimiter (`<<EOF`) parsing hazards within YAML block scalars.
- **Sequential Dual-Architecture Builds in Single Pipeline**:
  To avoid redundant test execution, frontend tests, Go tests, and embedded asset checks execute once. The pipeline then builds the Linux amd64 `.so`, installs the arm64 cross-toolchain (`gcc-aarch64-linux-gnu libc6-dev-arm64-cross`), and builds the Linux arm64 `.so` using `CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc`.
- **Pre-Upload ELF Architecture & glibc Linkage Verification**:
  Before any artifact upload, the pipeline inspects both output binaries using `readelf -h`, `file`, and `readelf -d` to verify:
  - 64-bit ELF shared object format (`ELF64`, `DYN`).
  - Architecture headers: x86-64 for amd64, AArch64 for arm64.
  - Dynamic linkage against GNU C Library (`libc.so.6`) and absence of musl linkage.
- **Exclusion of C Header Artifacts**:
  Go C-shared compilation generates companion `.h` header files. The workflow explicitly uploads only `.so` shared library files, omitting unneeded header artifacts from releases.
- **Cryptographic Checksumming**:
  Per-architecture SHA-256 checksum files (`visual-profile-linux-amd64.so.sha256` and `visual-profile-linux-arm64.so.sha256`) are computed and attached to the GitHub Release alongside each binary.
- **Shell Injection Immunity**:
  All inputs and GitHub context variables are bound via environment variables (`env:`). Direct script interpolation (`${{ ... }}` inside `run:` blocks) is strictly forbidden.

---

## 2. GitHub Permissions, Rulesets & Access Requirements

### Dispatch Rights
- **Collaborator Access**:
  On GitHub, the "Run workflow" button for `workflow_dispatch` is available only to repository collaborators with `write` or `admin` permissions. External contributors and read-only users cannot dispatch the release workflow.
- **Branch Restriction**:
  The workflow enforces `github.ref == 'refs/heads/main'`. Triggering the workflow from a feature branch or custom ref will be rejected immediately by the workflow guard.

### GitHub Tag Ruleset Creation Restrictions on `GITHUB_TOKEN`
When GitHub Tag Rulesets are configured on the repository (for example, targeting `v*` tags with "Restrict creations" enabled):
- **`GITHUB_TOKEN` Enforcement**: GitHub evaluates rulesets against all API operations, including tag creation performed by `gh release create` using `GITHUB_TOKEN`.
- **Potential Creation Block**: If a ruleset restricts tag creation to specific roles or teams and does not permit GitHub Actions, the workflow's publish step will fail with an HTTP 403 or ruleset violation error when attempting to create the tag.
- **No Automatic Bypass**: The release workflow cannot automatically bypass tag rulesets.
- **Required Configuration**: Repository administrators must explicitly configure the repository Tag Ruleset's **Bypass list** or creation permissions to allow the release workflow / GitHub Actions actor to create tags matching `v*`.

### Branch Protection (`main`)
To ensure release workflow definitions, version declarations, and embedded assets cannot be modified without review:
1. Protect `main` with branch rules or rulesets.
2. Require pull request reviews and status check validations before merging.
3. Restrict direct pushes to `main`.

---

## 3. Partial Failure Handling & Recovery (Honest Partial Failure)

GitHub release creation with file attachments is a multi-step operation executed by GitHub CLI:
1. GitHub creates the release record and the git tag pointing to `--target`. Because `gh release create` is executed without `--draft`, this produces an immediately published release (not a draft).
2. GitHub CLI uploads attached release assets (`visual-profile-linux-amd64.so`, `visual-profile-linux-amd64.so.sha256`, `visual-profile-linux-arm64.so`, and `visual-profile-linux-arm64.so.sha256`) sequentially in separate requests. Creation and asset uploads are separate operations (there is no atomic tag+asset delivery).

### Partial Failure Scenario
If an asset upload fails midway (due to a transient network timeout, runner disruption, or GitHub asset service error) after the tag and release have been created:
- The git tag and published, possibly incomplete release may remain on GitHub without all assets attached.
- Subsequent reruns of the release workflow will **intentionally fail** during collision checks, because the tag and/or release now exists remotely.
- **No Automatic Deletion**: The workflow intentionally does not attempt automatic deletion of partial releases or tags to avoid unintended destructive actions or race conditions.

### Recovery Protocol
When a partial failure occurs, an authorized repository administrator must perform manual inspection:
1. Navigate to **Releases** on GitHub and inspect the affected release and its attached assets.
2. If any binary or checksum is missing:
   - **Option A (Complete the release)**: Manually upload the missing asset files directly via the GitHub web UI or `gh release upload <tag> <files>`.
   - **Option B (Clean removal and rerun)**: If choosing deletion, the administrator must remove **BOTH** the GitHub Release object and the remote tag after inspecting:
     - Delete the GitHub Release object via the GitHub web UI or `gh release delete <tag>`.
     - Delete the remote tag via authorized tag deletion where permitted (via GitHub web UI or `git push origin --delete <tag>`), subject to repository tag ruleset permissions.
     - *Important*: An unqualified single tag deletion is never sufficient on its own, because leaving the GitHub Release object will continue to fail the workflow's fail-closed release collision check. Both must be removed.
     - Once both are cleanly deleted, resolve the root cause of the upload disruption and re-dispatch the release workflow from `main`.

---

## 4. Integrity, Provenance & Validation Pipeline

The workflow executes two strictly isolated jobs:

```
[workflow_dispatch on main with new tag input vX.Y.Z]
        │
        ▼
[build job] (permissions: contents: read)
  ├── 1. Verify repository (gonzalez962/cpamc-auto-switcher) and branch (refs/heads/main)
  ├── 2. Validate input tag format strictly: ^v[0-9]+\.[0-9]+\.[0-9]+$
  ├── 3. Checkout repository (fetch-depth: 0)
  ├── 4. Pin checked-out SHA to workflow_dispatch commit and verify origin/main ancestry
  ├── 5. Check remote tag collision (fail-closed): git ls-remote origin refs/tags/TAG (must be exit 2)
  ├── 6. Check release collision (fail-closed): gh api repos/${REPO}/releases/tags/${TAG} (must return HTTP 404)
  ├── 7. Validate Go version parity (internal/version/version.go == TAG#v)
  ├── 8. Validate web package.json version parity (web/package.json == TAG#v)
  ├── 9. Set up Node.js 20 with npm cache
  ├── 10. Set up Go 1.22
  ├── 11. Install web dependencies (npm ci) & run web tests (npm test -- --run)
  ├── 12. Build web assets (npm run build)
  ├── 13. Embedded asset parity check: git diff --exit-code -- internal/web/assets/index.html
  ├── 14. Run Go unit tests: go test -v ./...
  ├── 15. Build Linux amd64 shared library: go build -buildmode=c-shared ...
  ├── 16. Install arm64 cross-toolchain (gcc-aarch64-linux-gnu libc6-dev-arm64-cross)
  ├── 17. Build Linux arm64 shared library: CC=aarch64-linux-gnu-gcc go build -buildmode=c-shared ...
  ├── 18. Verify ELF binary architecture and glibc linkage (readelf -h, file, readelf -d)
  ├── 19. Upload Linux amd64 .so artifact (no .h files)
  └── 20. Upload Linux arm64 .so artifact (no .h files)
        │
        ▼
[publish job] (permissions: contents: write)
  ├── 1. Verify repository authorization
  ├── 2. Checkout verified commit SHA (needs.build.outputs.commit_sha)
  ├── 3. Download Linux amd64 artifact
  ├── 4. Download Linux arm64 artifact
  ├── 5. Generate per-arch SHA256 checksums (.so.sha256)
  ├── 6. Recheck remote tag collision (fail-closed): git ls-remote origin (must be exit 2)
  ├── 7. Recheck release collision (fail-closed): gh api (must return HTTP 404)
  ├── 8. Verify origin/main still contains target commit (git merge-base --is-ancestor)
  ├── 9. Generate release notes with provenance metadata using safe printf formatting
  └── 10. Create GitHub Release & Tag: gh release create WITHOUT --verify-tag, attaching both .so and .sha256 files
```

---

## 5. Step-by-Step Release Procedure

### Step 1: Prepare Code and Version Parity on `main`
1. Update internal version declarations to the target release version `X.Y.Z`:
   - `plugins/visual-profile/internal/version/version.go`:
     ```go
     const Version = "0.1.0"
     ```
   - `plugins/visual-profile/web/package.json`:
     ```json
     "version": "0.1.0"
     ```
   - Synchronize `package-lock.json`: Both the root `"version"` and `packages[""].version` in `plugins/visual-profile/web/package-lock.json` must match the web `package.json` version. Run `npm install --package-lock-only` (or `npm version <X.Y.Z> --no-git-tag-version`) before running `npm ci`, and commit the updated lockfile:
     ```bash
     cd plugins/visual-profile/web
     npm install --package-lock-only
     ```
2. Rebuild frontend assets and verify embedding:
   ```bash
   npm ci
   npm test -- --run
   npm run build
   cd ../../..
   git diff --exit-code -- plugins/visual-profile/internal/web/assets/index.html
   ```
3. Run Go tests locally:
   ```bash
   cd plugins/visual-profile
   go test -v ./...
   cd ../..
   ```
4. Commit changes (including the updated `package-lock.json`), open a pull request, review, and merge into `main`.
   > **Note**: No local tag push required. Do **not** create or push a Git tag locally. The release workflow will create the tag on GitHub automatically.

### Step 2: Trigger the Release via `workflow_dispatch`
1. In the GitHub repository, navigate to the **Actions** tab.
2. In the left sidebar, select **Release Linux Visual Profile Plugin**.
3. Click the **Run workflow** dropdown on the right:
   - Ensure **Branch: main** is selected.
   - In the **Release version tag** input, enter the new version tag: `v0.1.0`.
   - Click the green **Run workflow** button.
4. The workflow will:
   - Validate repository, branch (`refs/heads/main`), and tag format (`vX.Y.Z`).
   - Pin the checked-out commit SHA to `main` before mutation.
   - Verify the tag and release do not already exist on remote origin (fail-closed).
   - Check version parity in Go and web code.
   - Run tests, build web assets, verify asset parity, and compile both amd64 and arm64 `.so` libraries.
   - Verify ELF architectures and glibc linkage via `readelf -h`, `file`, and `readelf -d`.
   - Recheck collisions in the publish job.
   - Create the release and tag at the tested commit via `gh release create` and upload `.so` binaries and `.sha256` checksums.

### Step 3: Verify Published Release and Assets
After the workflow completes successfully, verify the published release:
1. Check the GitHub Releases page for `v0.1.0`.
2. Confirm the tag was created at the target commit on `main`.
3. Confirm all four release assets are attached (no `.h` headers):
   - `visual-profile-linux-amd64.so`
   - `visual-profile-linux-amd64.so.sha256`
   - `visual-profile-linux-arm64.so`
   - `visual-profile-linux-arm64.so.sha256`
4. Checksum verification commands for each architecture:

   **For Linux amd64**:
   ```bash
   sha256sum -c visual-profile-linux-amd64.so.sha256
   ```
   Expected output:
   ```text
   visual-profile-linux-amd64.so: OK
   ```

   **For Linux arm64 (glibc)**:
   ```bash
   sha256sum -c visual-profile-linux-arm64.so.sha256
   ```
   Expected output:
   ```text
   visual-profile-linux-arm64.so: OK
   ```

5. Target Environment Compatibility:
   - **Linux amd64**: Standard x86_64 Linux servers and desktops (Debian, Ubuntu, RHEL, CentOS, Fedora) with glibc.
   - **Linux arm64 (glibc)**: ARM64 / AArch64 Linux environments running glibc:
     - Raspberry Pi OS (64-bit / aarch64)
     - AWS Graviton / Graviton2 / Graviton3 (Amazon Linux 2023, Ubuntu arm64)
     - Apple Silicon Docker containers running Linux/arm64 guests (e.g. `linux/arm64` container images on Docker Desktop, OrbStack, Colima)
   - **Unsupported Environments**:
     - **macOS host native runtime**: The `visual-profile-linux-arm64.so` binary is an ELF shared library for Linux glibc. Native macOS (`darwin/arm64`) cannot load ELF `.so` files and requires Mach-O `.dylib` plugins.
     - **musl-based distributions**: Alpine Linux and other musl-libc environments cannot load glibc-linked shared libraries without compatibility layers (e.g. `gcompat`).
