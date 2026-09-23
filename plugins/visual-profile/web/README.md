# Visual Profile Web UI

Interactive React Flow prefix topology editor for CLIProxyAPI parent/child account profiles.

## Architecture

- **Framework**: Vite + React 18
- **Graph Engine**: `@xyflow/react`
- **Naming Rule**: When a parent node (e.g. `agy_p1`, `agy_p2`) is connected to a child node, the child's prefix and label are assigned `${parentPrefix}_${nextNumber}`, where `nextNumber` is `exact outgoing edges from source in eds + 1`.
- **Underscore Preservation**: Underscores in parent prefixes are never stripped or interpreted as suffixes. Full prefixes like `agy_p1` produce `agy_p1_1`, `agy_p1_2`, and complex prefixes like `custom_pool_prod_01` produce `custom_pool_prod_01_1`.
- **Single-Parent Topology Guard**: Children in prefix pools belong to exactly one parent. Connecting a second parent to an already-linked child or repeating a connection is rejected without renaming the child.
- **Derived Outgoing Counts**: Outgoing counts shown on parent nodes are derived dynamically from `eds` (`countOutgoingEdges(edges, node.id)`), eliminating redundant mutable state on node data.
- **Atomic Reducer Architecture & Deviation Note**:
  - *Architectural Rationale*: A pair of independent `setNodes` and `setEdges` updater functions in `onConnect` cannot coordinate state atomically in React. `useState` updaters run deferred during render and are re-executed under StrictMode; sharing closure variables or mutable refs between them introduces race conditions, stale counts, and `null` values on rapid batch updates.
  - *Honest Deviation*: ProfileGraph intentionally uses a single atomic graph reducer (`useReducer(graphReducer)`) owning both `nodes` and `edges`. Inside `onConnect`, an atomic `CONNECT` action derives parent prefix from current `nds` and outgoing count from current `eds`.
  - *Functional Compatibility*: Functional `setNodes` and `setEdges` adapter wrappers are retained for React Flow event compatibility (`applyNodeChanges`, `applyEdgeChanges`).
- **Constraints**: Local visual editor only; does not perform auth-file API mutations. Bundled completely locally with zero external CDN dependencies.

## Development & Verification Commands

```bash
# Run tests
npm test -- --run

# Build embedded static assets into ../internal/web/assets
npm run build
```
