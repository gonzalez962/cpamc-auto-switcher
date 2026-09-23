# Visual Profile Plugin

Standalone CLIProxyAPI C-ABI plugin providing an embedded React Flow visual editor for prefix-based parent and child account routing.

## Overview

The `visual-profile` plugin integrates with the CLIProxyAPI host via C-ABI version 1 and provides a management UI at `/profiles`. It enables visual management of parent/child profile topologies while strictly preserving full parent prefixes (such as `agy_p1`, `agy_p2`, or complex multi-underscore prefixes) when assigning child prefixes (`${parentPrefix}_${nextNumber}`).

## Key Features

- **CLIProxyAPI C-ABI v1**:
  - Implements standard C-ABI functions: `cliproxy_plugin_init`, `cliproxyPluginCall`, `cliproxyPluginFree`, and `cliproxyPluginShutdown`.
  - Exported through `main.go` using Cgo with C-ABI v1 structs (`cliproxy_host_api`, `cliproxy_plugin_api`, `cliproxy_buffer`).
- **Audited Management Endpoint (`/profiles`)**:
  - **Exact Segment Routing**: Enforces exact `/profiles` segment matching; paths where `profiles` is only a substring (e.g. `/profiles_evil`, `/myprofiles`) are rejected with HTTP 404.
  - **Method Guard**: Restricts requests to `GET` and `HEAD`; unsupported HTTP methods (`POST`, `PUT`, `DELETE`) return HTTP 405 Method Not Allowed. HEAD returns headers with an empty body.
  - **Hostile Path & Traversal Protection**: Rejects directory traversal attempts (`..`, `%2e%2e`, backslashes, null bytes) with HTTP 400 Bad Request.
  - **Embedded Static Assets**: Resolves static assets (HTML, CSS, JS, SVG, JSON) from embedded storage via Go `//go:embed` with security headers (`X-Content-Type-Options: nosniff`).
- **Vite React + @xyflow/react Web UI**: High-performance interactive graph canvas with dark theme styling matching CPAMC.
- **Prefix Connection Convention**:
  - Connect a parent node (e.g. `agy_p1`) to a child node.
  - The child is assigned `${parentPrefix}_${nextNumber}`, where `nextNumber` is the count of exact outgoing edges from the source in `eds` + 1 (`agy_p1_1`, `agy_p1_2`, etc.).
  - Underscores are preserved completely without suffix truncation or parsing.
- **Single-Parent Topology Guard**:
  - Enforces tree topology for prefix pools: each child node can belong to only one parent prefix pool.
  - Duplicate connections and attempts to connect a second parent to an already-connected child are rejected atomically without renaming the child.
- **Atomic Graph Reducer Architecture & Deviation Note**:
  - *Architectural Rationale*: The initial task prompt proposed using a pair of `setNodes` and `setEdges` state updater functions inside `onConnect`. In React 18+ and StrictMode, independent `useState` updater functions execute deferred during the render phase and cannot atomically coordinate state across hooks without shared mutable variables or refs—an impure anti-pattern causing race conditions, stale edge counts, and `null` prefixes under rapid batching.
  - *Honest Deviation*: To guarantee strict purity and correctness, `ProfileGraph` uses an atomic graph state reducer (`useReducer`) as the single source of truth for both `nodes` and `edges`. Inside `onConnect`, an atomic `CONNECT` action derives parent prefix from current `nds` and outgoing edge count from current `eds` in a single pure transition. Node IDs are generated in event handlers before dispatch, ensuring complete reducer idempotence under React StrictMode replay.
  - *Functional Compatibility*: Functional `setNodes` and `setEdges` adapter wrappers are retained for React Flow event compatibility (`applyNodeChanges`, `applyEdgeChanges`).
- **Derived Outgoing Counts**:
  - Outgoing child counts displayed in node footers are derived dynamically from `eds` (`countOutgoingEdges(edges, node.id)`), removing redundant, potentially stale counters from node data.
- **Explicit Save Changes & Management PATCH (VP-4)**:
  - Supports authenticated persistence of dirty auth file prefixes via `PATCH /v0/management/auth-files/fields`.
  - Stages changes locally with dirty tracking; strictly no auto-PATCH on keystroke.
  - Reconciles saved state using submitted values without losing concurrent in-flight edits.
  - Multi-file partial failure handling via `Promise.allSettled` with per-file sanitized errors (401/404/409) and selective retry.
  - Local-only demo remains non-persistable; synthetic nodes are never sent to the Management API.
- **Zero External CDN Dependencies**: All assets (HTML, CSS, JS, fonts) are bundled locally.

## Project Structure

```
plugins/visual-profile/
├── go.mod               # Standalone Go module (visual-profile)
├── main.go              # C-ABI export functions (cliproxy_plugin_init, cliproxyPluginCall, etc.)
├── main_nocgo.go        # Pure Go fallback stub for non-CGO testing
├── main_test.go         # Integration tests for plugin methods, routing, and registration
├── README.md            # Plugin documentation
├── internal/
│   ├── handlers/        # HTTP resource handler and security headers for /profiles
│   ├── plugin/          # Plugin registration, envelope handling, management routing
│   ├── version/         # Semantic versioning
│   └── web/             # Embedded web assets (embed.go) and asset directory
└── web/                 # Vite + React + @xyflow/react web frontend
    ├── package.json
    ├── vite.config.js
    ├── index.html
    ├── README.md
    └── src/
        ├── App.jsx
        ├── App.css
        ├── main.jsx
        ├── components/  # ProfileGraph, ProfileNode, onConnect tests
        └── graph/       # Pure connection functions, graphReducer, Vitest suites
```

## Verification & Testing

### Web Frontend Tests & Build

```bash
cd plugins/visual-profile/web

# Run Vitest test suite
npm test -- --run

# Compile static assets into ../internal/web/assets
npm run build
```

### Go Plugin Tests

```bash
cd plugins/visual-profile

# Run all package unit and integration tests
go test ./...
```

### C-Shared Library Build (CGO required)

To compile the C-ABI shared library for CLIProxyAPI, CGO must be enabled and an appropriate GCC toolchain installed:

```bash
cd plugins/visual-profile

# Linux AMD64 build (native on Linux or via cross-compiler)
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o visual-profile-linux-amd64.so .

# Windows AMD64 build (with MinGW-w64 gcc)
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -buildmode=c-shared -o visual-profile-windows-amd64.dll .
```
