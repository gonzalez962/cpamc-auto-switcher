# Visual Profile Web UI

Interactive React Flow prefix topology editor for CLIProxyAPI parent/child account profiles.

## Architecture

- **Framework**: Vite + React 18
- **Graph Engine**: `@xyflow/react`
- **Naming Rule**: When a parent node (e.g. `agy_p1`, `agy_p2`) is connected to a child node, the child's prefix and label are assigned `${parentPrefix}_${nextNumber}`, where `nextNumber` is `exact outgoing edges from source in eds + 1`.
- **Underscore Preservation**: Underscores in parent prefixes are never stripped or interpreted as suffixes. Full prefixes like `agy_p1` produce `agy_p1_1`, `agy_p1_2`, and complex prefixes like `custom_pool_prod_01` produce `custom_pool_prod_01_1`.
- **Single-Parent Topology Guard**: Children in prefix pools belong to exactly one parent. Connecting a second parent to an already-linked child or repeating a connection is rejected without renaming the child.
- **Derived Outgoing Counts**: Outgoing counts shown on parent nodes are derived dynamically from `eds` (`countOutgoingEdges(edges, node.id)`), eliminating redundant mutable state on node data.
- **Atomic Reducer Architecture**:
  - ProfileGraph uses an atomic graph reducer (`useReducer(graphReducer)`) owning both `nodes` and `edges`.
  - Inside `onConnect`, an atomic `CONNECT` action derives parent prefix from current `nds` and outgoing count from current `eds`.
  - Functional `setNodes` and `setEdges` adapter wrappers are retained for React Flow event compatibility (`applyNodeChanges`, `applyEdgeChanges`).

## Management API Client & Authentication (VP-3)

- **Authentication & Key Extraction**:
  - Retrieves the `managementKey` from CPAMC parent `localStorage` (iframe host) or local window `localStorage`.
  - Deobfuscates CPAMC storage values using the reference XOR cipher with `enc::v1::` envelope and host/userAgent context.
  - **Fail-Closed Guard**: Fails closed when no valid management key is present, rendering an authentication requirement view with clear steps to log in with "Remember password" checked in CPAMC.
- **Same-Origin Enforcement**:
  - Enforces strict same-origin requests (`assertSafeSameOrigin`). Protocol-relative (`//`) or cross-origin URLs are rejected.
  - Authorization header: `Authorization: Bearer <key>`.
  - Error messages are sanitized: Bearer keys, sensitive query params, and raw response dumps are never leaked in error outputs or console.
- **Auth-File Listing & Prefix Extraction**:
  - `GET /v0/management/auth-files`: Retrieves registered auth file metadata.
  - `GET /v0/management/auth-files/download?name=<encodedName>`: Downloads raw credential JSON.
  - **Strict Credential Isolation**: Only the `prefix` field is extracted and stored. Sensitive tokens, client secrets, refresh tokens, and private keys are discarded immediately and never stored in node state, application state, or logs.

## Graph Loading & Node Editing (VP-3)

- **Immutable Exact File Name IDs**:
  - Nodes created from auth files use exact file names as their immutable IDs (e.g. `antigravity-1.json`, `codex-prod.json`).
  - Pre-existing prefix relationships matching `${parentPrefix}_${index}` automatically reconstruct visual edges on initial load.
- **Fail-Closed Auth-File Listing & Prefix Invariants**:
  - `listAuthFiles` strictly enforces physical filename presence and fails closed on malformed listing items (never falls back to entity index `id`).
  - **Legitimate Duplicate Prefixes & Ambiguity Defense**: In CLIProxyAPI, duplicate prefixes are legitimate for account rotation pools (e.g. multiple accounts sharing `agy_p1`). Blanket unique-prefix lockouts are avoided. When reconstructing parent/child edges, if candidate parent prefix is shared by multiple files, graph inference does NOT invent parent edges; ambiguous nodes remain safely disconnected and visibly explained in the node badge (`AMBIGUOUS PARENT`) and footer notice without guessing.
  - Unambiguous deterministic edge identity is enforced across graph builders and connection reducers (`edge__${source}-${target}`).
- **Graph & Prefix Coherence Invariants**:
  - **Parent Rename Protection**: Renaming a parent node while attached children exist is rejected in both the reducer and UI to prevent stale or orphaned descendant namespaces. The UI explicitly explains: *"Cannot rename parent with attached children. Disconnect children first."*
  - **Child Manual Edit Detachment**: Manually editing a child node's prefix automatically detaches its incoming edge, ensuring independent nodes do not retain a false parent relationship.
  - **Empty Source Prefix Defense**: Attempting to connect a child to a parent with an empty prefix is rejected (`source_prefix_empty`), strictly preventing fallbacks to filename or label.
  - **Edge Removal Collision Defense**: When edges are removed, subsequent child connections evaluate all existing prefixes across nodes to allocate the next unused sequence number (preserving `count + 1` when available), preventing duplicate child prefix collisions.
  - **Dirty State Protection on Reset / Reload**: Resetting the graph or reloading auth files queries node state imperatively (`graphRef.current.getNodes()` / `isDirty()`, avoiding React render lag) and prompts for confirmation if unsaved modifications exist, preventing accidental data loss.
  - **Saved Graph Baseline Synchronization & Re-clobber Prevention**: After successful saves, the reset baseline is synchronized with the newly saved prefixes and freshest graph state (preserving in-flight edge changes). Baseline re-clobber on component rerender is eliminated; baseline resets only on explicit dataset remount (`key` versioning).
  - **Concurrency & Race Guards**: Reset during active save is disabled, and reloading while a save is in flight is guarded against race conditions.
- **Prevention of Synthetic Nodes in Host UI**:
  - In production mode (`allowSynthetic={false}`), arbitrary unbound creation buttons (`+ Add Root Profile`, `+ Add Child Node`) are hidden, ensuring users cannot create unbacked nodes that cannot be saved to the Management API.
  - Synthetic test nodes carry `isSynthetic: true` for safe test execution.
- **Inline Prefix Editing in ProfileNode**:
  - Double-click or click the edit button (✏️) on any node to enter inline edit mode.
  - Inputs use `nodrag` and `nopan` class names to prevent React Flow drag and canvas panning gestures while typing.
  - Validation: empty prefix `""` is allowed per API ("exact allowed empty"), and nonempty strings must contain only alphanumeric characters, underscores, or hyphens (`[a-zA-Z0-9_-]+`).
  - Atomically updates node prefix, label, and dirty state via `UPDATE_NODE_PREFIX` reducer action.
  - Connecting real nodes updates child's prefix and sets its dirty state (`isDirty`) atomically when the prefix differs from the file's initial prefix.
  - Visual indicators: `DIRTY` badge on modified nodes and dynamic count in the status bar.

## Explicit Save Changes & Management PATCH (VP-4)

- **Explicit Save Action**:
  - Live auth mode (`allowSynthetic={false}`) renders an explicit **Save Changes** button in the graph toolbar.
  - The button is disabled when `dirtyCount === 0` or while a save request is in flight (`isSaving`).
  - **No Auto-PATCH on Keystroke**: Edits made via inline typing or connection assignment are staged in local React Flow state; network requests are dispatched ONLY on explicit user save.
  - **Non-Persistable Offline Demo**: In demo mode (`allowSynthetic={true}`), the Save Changes action is completely hidden and programmatic save attempts reject, keeping offline exploration strictly local and non-persistable.
  - **Synthetic Node Filter**: Nodes marked `isSynthetic: true` are strictly filtered out and never sent to the Management API.
- **Management PATCH Endpoint & Error Sanitization**:
  - Dispatches `PATCH /v0/management/auth-files/fields` with `{ name, prefix }` for each dirty REAL auth file.
  - Uses exact physical filename (e.g. `antigravity-1.json`) and current prefix (including empty string `""` explicitly allowed).
  - Authenticates same-origin via `managementClient` `apiFetch` with `Authorization: Bearer <key>`.
  - Sanitized Error Handling: Handles HTTP 401 Unauthorized, 404 Not Found, and 409 Conflict. Error messages display sanitized filenames and error descriptions without leaking bearer keys, credential bodies, or tokens.
- **State Reconciliation & Draft Preservation (CRITICAL)**:
  - **Zero Lost Edits During In-Flight Requests**: If a user continues typing or connecting nodes while a save request is pending in the network, those concurrent draft edits are NEVER overwritten or lost upon server response.
  - **Atomic Reducer Dispatch (`RECONCILE_SAVED_NODES`)**:
    - For successful files: Advances `initialPrefix` to the submitted prefix value that succeeded on the server. The node's current `prefix` (latest draft) is preserved. If the current draft equals the submitted value, `isDirty` becomes `false`; if the user edited the node while pending, `isDirty` remains `true`.
    - For failed files: The node retains its original `initialPrefix` and remains `isDirty: true`.
    - Does NOT blindly reset the graph: Canvas zoom, viewport position, node coordinates, edge connections, and unmodified nodes are completely preserved.
  - **Overlapping Save Protection**: Overlapping save invocations while a request is in flight are strictly blocked via `isSavingRef` and UI disabled state.
- **Multi-File Partial Failure & Retry**:
  - **No Claimed Atomicity**: The CLIProxyAPI Management API updates auth files individually. The plugin does NOT claim cross-file atomicity or rollback.
  - Uses `Promise.allSettled` to process dirty files independently.
  - If some files succeed and others fail (e.g. 409 Conflict due to backend state conflicts or 404), the UI renders a **Partial Failure** banner with a detailed, sanitized failure breakdown.
  - A **Retry** button is rendered on partial or complete failure. Clicking Retry triggers save for ONLY the remaining dirty files, as successful files have already advanced their `initialPrefix` and are no longer dirty.
  - **Pool Rotation Support**: Multiple accounts sharing a prefix pool (e.g. rotation accounts) are supported without client-side unique-prefix lockout.

## Single-File Self-Contained Bundle (VP-6)

- **Single-File Architecture**:
  - `vite-plugin-singlefile` inlines all CSS (`<style>`) and JavaScript (`<script>`) directly into a single self-contained `index.html`.
  - External browser subresource requests (`<script src="...">`, `<link rel="stylesheet" ...>`, `<link rel="icon" ...>`) are completely eliminated.
  - Vite `modulePreload` polyfill is explicitly disabled (`modulePreload: { polyfill: false }`), removing dead `fetch()` calls for module scripts.
  - Zero runtime CDN or remote resource dependencies; single HTML unchanged semantics authored by `gonzalez962`.
- **Host Route Alignment (Slashless Runtime vs. Internal Isolation)**:
  - The CLIProxyAPI host (`ServeResourceHTTP`) resolves resources via exact route table lookup (`resourceRoutes[GET full r.URL.Path]`), where only the slashless `/profiles` route is registered.
  - **Host-Facing Runtime URL**: Only slashless `/profiles` is supported at runtime through CLIProxyAPI; runtime requests with a trailing slash (`/profiles/`) are not matched by the host router and return 404.
  - **Internal Handler Isolation**: The internal plugin handler (`MatchProfilesRoute`) tolerates a trailing slash in isolation as a defensive fallback, but this is an internal test/handler detail and not a supported host runtime route.
  - Subresource paths such as `/assets/...` or `/profiles/assets/...` are rejected before reaching plugin handlers.
  - Delivering a fully self-contained `index.html` allows the entire visual topology editor to load and function properly under the exact slashless `/profiles` host route without secondary network fetches.

## Development & Verification Commands

```bash
# Run tests
npm test -- --run

# Build embedded static assets into ../internal/web/assets
npm run build
```
