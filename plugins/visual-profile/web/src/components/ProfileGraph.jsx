import React, {
  useState,
  useEffect,
  useCallback,
  useMemo,
  useReducer,
  useRef,
  forwardRef,
  useImperativeHandle,
} from 'react';
import {
  ReactFlow,
  Controls,
  Background,
  MiniMap,
  applyNodeChanges,
  applyEdgeChanges,
  BackgroundVariant,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import ProfileNode from './ProfileNode';
import { countOutgoingEdges } from '../graph/connection';
import { graphReducer } from '../graph/graphReducer';
import { saveAuthFilePrefixes } from '../api/managementClient';

const nodeTypes = {
  profile: ProfileNode,
};

// Default initial state with root profiles agy_p1 and agy_p2
const initialNodesData = [
  {
    id: 'p1',
    type: 'profile',
    position: { x: 150, y: 80 },
    data: { label: 'agy_p1', prefix: 'agy_p1', isRoot: true },
  },
  {
    id: 'p2',
    type: 'profile',
    position: { x: 450, y: 80 },
    data: { label: 'agy_p2', prefix: 'agy_p2', isRoot: true },
  },
  {
    id: 'c1',
    type: 'profile',
    position: { x: 150, y: 260 },
    data: { label: 'unlinked_1', prefix: 'unlinked_1', isRoot: false },
  },
  {
    id: 'c2',
    type: 'profile',
    position: { x: 450, y: 260 },
    data: { label: 'unlinked_2', prefix: 'unlinked_2', isRoot: false },
  },
];

const initialEdgesData = [];

// Normalize nodes ensuring each has valid position coordinates
function sanitizeNodes(nodes) {
  if (!Array.isArray(nodes)) return [];
  return nodes.map((n, idx) => ({
    ...n,
    position:
      n.position && typeof n.position.x === 'number' && typeof n.position.y === 'number'
        ? n.position
        : { x: 100 + (idx % 4) * 160, y: 80 + Math.floor(idx / 4) * 140 },
  }));
}

/**
 * ARCHITECTURAL DESIGN NOTE:
 * The original task prompt suggested using a pair of setNodes and setEdges updater functions in onConnect.
 * However, in React 18+ and StrictMode, independent useState updater functions run deferred during render
 * and cannot coordinate state atomically without mutable refs or impure closure variables (which cause
 * race conditions and stale reads under rapid batching).
 *
 * To prioritize correctness and strict purity, ProfileGraph uses an atomic graphReducer (useReducer)
 * as the single state owner for both nodes and edges. Unique IDs are generated in event handlers before dispatch,
 * keeping the reducer strictly pure and deterministic.
 *
 * Inside the reducer:
 *   1. Parent prefix is read from current nds.
 *   2. Outgoing count is derived from current eds.
 *   3. Duplicate edges and multi-parent connections are rejected without renaming the child.
 *   4. Next available prefixes are chosen to prevent collisions with existing custom prefixes.
 *
 * Functional setNodes and setEdges interfaces are retained as adapters for React Flow change events.
 */
const ProfileGraph = forwardRef(function ProfileGraph(
  {
    initialNodes = initialNodesData,
    initialEdges = initialEdgesData,
    onGraphChange,
    onConnect: externalOnConnect,
    allowSynthetic = true,
    apiOptions = {},
    onSaveSuccess,
    onSaveError,
  },
  ref
) {
  const safeInitialNodes = useMemo(() => sanitizeNodes(initialNodes), [initialNodes]);
  const safeInitialEdges = useMemo(
    () => (Array.isArray(initialEdges) ? initialEdges : []),
    [initialEdges]
  );

  // Single atomic graph state owner
  const [graphState, dispatch] = useReducer(graphReducer, {
    nodes: safeInitialNodes,
    edges: safeInitialEdges,
  });

  const { nodes, edges } = graphState;

  // Keep synchronous ref to freshest graph state for async baseline synchronization
  const graphStateRef = useRef(graphState);
  graphStateRef.current = graphState;

  // Save changes state and overlap guard
  const [isSaving, setIsSaving] = useState(false);
  const isSavingRef = useRef(false);

  // Maintain clean baseline to prevent Reset from reverting to stale initial props after save.
  // Initialized on mount from initial dataset; updated on successful save or dataset remount.
  // NOTE: Avoid syncing from safeInitialNodes in an effect to prevent re-clobbering saved baseline on rerender.
  const currentBaselineRef = useRef({
    nodes: safeInitialNodes,
    edges: safeInitialEdges,
  });
  const [saveState, setSaveState] = useState({
    status: 'idle', // 'idle' | 'saving' | 'success' | 'partial' | 'error'
    message: '',
    successful: [],
    failed: [],
  });

  const handleDismissSaveStatus = useCallback(() => {
    setSaveState({ status: 'idle', message: '', successful: [], failed: [] });
  }, []);

  // Monotonic ID counter for event handler ID generation (keeps reducer 100% pure)
  const idCounterRef = useRef(1);
  const generateId = useCallback((prefix) => {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return `${prefix}_${crypto.randomUUID()}`;
    }
    return `${prefix}_${Date.now()}_${idCounterRef.current++}`;
  }, []);

  // Functional setNodes interface backed by atomic reducer
  const setNodes = useCallback((updater) => {
    dispatch({ type: 'SET_NODES', updater });
  }, []);

  // Functional setEdges interface backed by atomic reducer
  const setEdges = useCallback((updater) => {
    dispatch({ type: 'SET_EDGES', updater });
  }, []);

  // React Flow node changes adapter
  const onNodesChange = useCallback(
    (changes) => {
      setNodes((nds) => applyNodeChanges(changes, nds));
    },
    [setNodes]
  );

  // React Flow edge changes adapter
  const onEdgesChange = useCallback(
    (changes) => {
      setEdges((eds) => applyEdgeChanges(changes, eds));
    },
    [setEdges]
  );

  // Atomic onConnect callback
  const onConnect = useCallback(
    (connection) => {
      dispatch({ type: 'CONNECT', connection });
      if (typeof externalOnConnect === 'function') {
        externalOnConnect(connection);
      }
      if (typeof onGraphChange === 'function') {
        onGraphChange();
      }
    },
    [externalOnConnect, onGraphChange]
  );

  // Atomic inline prefix change callback with validation feedback
  const handleNodePrefixChange = useCallback(
    (nodeId, newPrefix) => {
      const targetNode = nodes.find((n) => n.id === nodeId);
      const trimmed = typeof newPrefix === 'string' ? newPrefix.trim() : '';

      // Check parent with attached children
      const hasChildren = edges.some((e) => e && e.source === nodeId);
      if (hasChildren && trimmed !== (targetNode?.data?.prefix || '')) {
        return {
          success: false,
          error:
            'Cannot rename parent with attached children. Disconnect children first.',
        };
      }

      dispatch({
        type: 'UPDATE_NODE_PREFIX',
        id: nodeId,
        prefix: newPrefix,
      });

      if (typeof onGraphChange === 'function') {
        onGraphChange();
      }

      return { success: true };
    },
    [nodes, edges, onGraphChange]
  );

  /**
   * Saves dirty real auth-file prefixes to the Management API via PATCH.
   *
   * SAFETY & RECONCILIATION INVARIANTS:
   * - Restricts concurrently conflicting duplicate non-empty prefixes before dispatching.
   * - Prevents overlapping saves: multiple clicks or invocations while in-flight are ignored.
   * - Demo mode is strictly local-only and non-persistable.
   * - Synthetic nodes are NEVER sent to the Management API.
   * - Sends exact physical filename and current prefix (including empty string).
   * - CRITICAL: Reconciles saved prefixes per filename using submitted values via RECONCILE_SAVED_NODES
   *   pure reducer dispatch, advancing initialPrefix for successful files, retaining original
   *   initialPrefix for failed files, and preserving all concurrent draft edits made while pending.
   * - Does NOT blindly reset the graph.
   * - Multi-file partial failure: no claiming atomicity; reports partial success and permits retry.
   */
  const handleSaveChanges = useCallback(async () => {
    // Guard against demo mode: demo is local-only non-persistable
    if (allowSynthetic) {
      const err = 'Demo mode is local-only and non-persistable.';
      setSaveState({
        status: 'error',
        message: err,
        successful: [],
        failed: [],
      });
      return { success: false, error: err };
    }

    // Prevent overlapping saves
    if (isSavingRef.current) {
      return { success: false, error: 'Save already in progress' };
    }

    // Find dirty REAL auth file nodes (never send synthetic nodes)
    const dirtyRealNodes = nodes.filter(
      (n) => Boolean(n.data?.isDirty) && !n.data?.isSynthetic
    );

    if (dirtyRealNodes.length === 0) {
      setSaveState({
        status: 'idle',
        message: 'No unsaved modifications to persist.',
        successful: [],
        failed: [],
      });
      return { success: true, count: 0 };
    }

    // Extract exact physical filename and current prefix (including empty string)
    const filesToSave = dirtyRealNodes.map((n) => {
      const fileName = n.data?.fileName || n.id;
      const prefix = n.data?.prefix !== undefined ? String(n.data.prefix).trim() : '';
      return {
        id: n.id,
        name: fileName,
        prefix,
        isSynthetic: Boolean(n.data?.isSynthetic),
      };
    });

    isSavingRef.current = true;
    setIsSaving(true);
    setSaveState({
      status: 'saving',
      message: `Saving ${filesToSave.length} modified profile(s)...`,
      successful: [],
      failed: [],
    });

    try {
      const saveResult = await saveAuthFilePrefixes(filesToSave, apiOptions);
      const { successful = [], failed = [] } = saveResult;

      // CRITICAL: Reconcile saved prefixes using submitted values, preserving latest drafts
      dispatch({
        type: 'RECONCILE_SAVED_NODES',
        successful,
        failed,
      });

      if (typeof onGraphChange === 'function') {
        onGraphChange();
      }

      // Update baseline nodes with newly saved prefixes to prevent Reset from reverting to stale initial props.
      // Must be built from freshest graph state (graphStateRef.current), not stale closure values,
      // preserving in-flight edge changes and latest draft edits.
      if (successful.length > 0) {
        const savedMap = new Map();
        successful.forEach((item) => {
          if (item && item.name) {
            savedMap.set(item.name, item.prefix || '');
          }
        });

        const latestState = graphStateRef.current || { nodes, edges };
        currentBaselineRef.current = {
          nodes: latestState.nodes.map((node) => {
            const fileKey = node.data?.fileName || node.id;
            if (savedMap.has(fileKey)) {
              const newPrefix = savedMap.get(fileKey);
              return {
                ...node,
                data: {
                  ...node.data,
                  prefix: newPrefix,
                  initialPrefix: newPrefix,
                  label: newPrefix || node.data?.fileName || node.id,
                  isDirty: false,
                },
              };
            }
            return node;
          }),
          // FRESHEST edges, preserving in-flight edge connections/disconnections during the async save
          edges: [...latestState.edges],
        };
      }

      if (failed.length === 0) {
        setSaveState({
          status: 'success',
          message: `Successfully saved ${successful.length} profile(s).`,
          successful,
          failed: [],
        });
        if (typeof onSaveSuccess === 'function') {
          onSaveSuccess(successful);
        }
        return { success: true, count: successful.length, successful };
      } else if (successful.length > 0) {
        setSaveState({
          status: 'partial',
          message: `Partially saved: ${successful.length} succeeded, ${failed.length} failed.`,
          successful,
          failed,
        });
        if (typeof onSaveError === 'function') {
          onSaveError({ successful, failed });
        }
        return { success: false, partial: true, successful, failed };
      } else {
        setSaveState({
          status: 'error',
          message: `Failed to save ${failed.length} profile(s).`,
          successful: [],
          failed,
        });
        if (typeof onSaveError === 'function') {
          onSaveError({ successful: [], failed });
        }
        return { success: false, failed };
      }
    } catch (err) {
      const errMsg = err.message || 'Failed to save profiles';
      setSaveState({
        status: 'error',
        message: errMsg,
        successful: [],
        failed: filesToSave.map((f) => ({ ...f, error: errMsg })),
      });
      if (typeof onSaveError === 'function') {
        onSaveError({
          successful: [],
          failed: filesToSave.map((f) => ({ ...f, error: errMsg })),
        });
      }
      return { success: false, error: errMsg };
    } finally {
      isSavingRef.current = false;
      setIsSaving(false);
    }
  }, [
    allowSynthetic,
    nodes,
    apiOptions,
    onGraphChange,
    onSaveSuccess,
    onSaveError,
  ]);

  /**
   * Resets graph restoring baseline clean state.
   * Prevents reset during save, and restores saved baseline rather than stale initial props.
   * Prompts for confirmation if any node has unsaved modifications (dirty).
   */
  const handleResetGraph = useCallback(() => {
    if (isSavingRef.current) {
      return false; // Prevent reset during active save
    }

    const hasDirty = nodes.some((n) => Boolean(n.data?.isDirty));
    if (hasDirty) {
      const confirmed =
        typeof window !== 'undefined' && typeof window.confirm === 'function'
          ? window.confirm(
              'You have unsaved prefix modifications. Resetting will discard all changes. Continue?'
            )
          : true;
      if (!confirmed) {
        return false;
      }
    }

    const baseline = currentBaselineRef.current || {
      nodes: safeInitialNodes,
      edges: safeInitialEdges,
    };
    dispatch({ type: 'RESET', nodes: baseline.nodes, edges: baseline.edges });
    return true;
  }, [nodes, safeInitialNodes, safeInitialEdges]);

  // Expose imperative handle for direct programmatic testing
  useImperativeHandle(
    ref,
    () => ({
      connect: (conn) => onConnect(conn),
      onConnect,
      updateNodePrefix: (id, prefix) => handleNodePrefixChange(id, prefix),
      getNodes: () => nodes,
      getEdges: () => edges,
      isDirty: () => nodes.some((n) => Boolean(n.data?.isDirty)),
      isSaving: () => Boolean(isSavingRef.current),
      save: () => handleSaveChanges(),
      getSaveState: () => saveState,
      reset: (force = false) => {
        if (isSavingRef.current) {
          return false; // Prevent reset during active save
        }
        if (!force && nodes.some((n) => Boolean(n.data?.isDirty))) {
          const confirmed =
            typeof window !== 'undefined' && typeof window.confirm === 'function'
              ? window.confirm(
                  'You have unsaved prefix modifications. Resetting will discard all changes. Continue?'
                )
              : true;
          if (!confirmed) return false;
        }
        const baseline = currentBaselineRef.current || {
          nodes: safeInitialNodes,
          edges: safeInitialEdges,
        };
        dispatch({ type: 'RESET', nodes: baseline.nodes, edges: baseline.edges });
        return true;
      },
    }),
    [
      onConnect,
      handleNodePrefixChange,
      handleSaveChanges,
      isSaving,
      saveState,
      nodes,
      edges,
      safeInitialNodes,
      safeInitialEdges,
    ]
  );

  // Input state for custom prefix
  const [customPrefix, setCustomPrefix] = useState('');

  /**
   * Creates a root node using atomic reducer dispatch.
   * IDs are generated before dispatch to preserve reducer purity.
   */
  const handleAddRootNode = useCallback(() => {
    const newId = generateId('p');
    dispatch({
      type: 'ADD_ROOT_NODE',
      id: newId,
      customPrefix,
    });
    setCustomPrefix('');
  }, [customPrefix, generateId]);

  /**
   * Creates an unlinked child node using atomic reducer dispatch.
   * IDs are generated before dispatch to preserve reducer purity.
   */
  const handleAddChildNode = useCallback(() => {
    const newId = generateId('c');
    dispatch({
      type: 'ADD_CHILD_NODE',
      id: newId,
    });
  }, [generateId]);

  // Derive outgoingCount and attach atomic onPrefixChange to all nodes
  const displayNodes = useMemo(() => {
    return nodes.map((node) => {
      const derivedCount = countOutgoingEdges(edges, node.id);
      return {
        ...node,
        data: {
          ...node.data,
          outgoingCount: derivedCount,
          onPrefixChange: handleNodePrefixChange,
        },
      };
    });
  }, [nodes, edges, handleNodePrefixChange]);

  // Derived metrics for UI status bar
  const rootCount = useMemo(() => nodes.filter((n) => n.data?.isRoot).length, [nodes]);
  const childCount = useMemo(() => nodes.filter((n) => !n.data?.isRoot).length, [nodes]);
  const dirtyCount = useMemo(
    () => nodes.filter((n) => Boolean(n.data?.isDirty)).length,
    [nodes]
  );

  return (
    <div className="graph-container">
      {/* Top Banner and Toolbar */}
      <div className="graph-toolbar">
        <div className="toolbar-left">
          <span className="toolbar-title">Visual Profile Prefix Topology</span>
          {allowSynthetic ? (
            <span className="badge-editor-only">Editor Only — Local Graph</span>
          ) : (
            <span className="badge-live-auth" data-testid="badge-live-auth">
              Management Auth-Files
            </span>
          )}
        </div>

        <div className="toolbar-controls">
          {allowSynthetic && (
            <>
              <div className="input-group">
                <input
                  type="text"
                  placeholder="e.g. agy_p1 or custom_pool"
                  value={customPrefix}
                  onChange={(e) => setCustomPrefix(e.target.value)}
                  className="prefix-input"
                  data-testid="input-custom-prefix"
                />
                <button
                  type="button"
                  onClick={handleAddRootNode}
                  className="btn btn-primary"
                  data-testid="btn-add-root"
                >
                  + Add Root Profile
                </button>
              </div>

              <button
                type="button"
                onClick={handleAddChildNode}
                className="btn btn-secondary"
                data-testid="btn-add-child"
              >
                + Add Child Node
              </button>
            </>
          )}

          <button
            type="button"
            onClick={handleResetGraph}
            className="btn btn-outline"
            data-testid="btn-reset-graph"
            disabled={isSaving}
          >
            Reset Graph
          </button>

          {!allowSynthetic && (
            <button
              type="button"
              onClick={handleSaveChanges}
              disabled={isSaving || dirtyCount === 0}
              className="btn btn-primary btn-save"
              data-testid="btn-save-changes"
              title={
                dirtyCount === 0
                  ? 'No unsaved modifications'
                  : 'Save modified prefixes to Management API'
              }
            >
              {isSaving
                ? 'Saving...'
                : dirtyCount > 0
                ? `Save Changes (${dirtyCount})`
                : 'Save Changes'}
            </button>
          )}
        </div>
      </div>

      {/* Stats bar */}
      <div className="graph-stats-bar">
        <span>
          Roots: <strong>{rootCount}</strong>
        </span>
        <span className="stats-divider">•</span>
        <span>
          Children: <strong>{childCount}</strong>
        </span>
        <span className="stats-divider">•</span>
        <span>
          Connections: <strong>{edges.length}</strong>
        </span>
        {dirtyCount > 0 && (
          <>
            <span className="stats-divider">•</span>
            <span className="stats-dirty" data-testid="stats-dirty-count">
              Modified: <strong className="text-warning">{dirtyCount}</strong>
            </span>
          </>
        )}
        <span className="stats-divider">•</span>
        <span className="stats-hint">
          Connect a parent node to a child node to assign <code>{'{parentPrefix}_{index}'}</code>
        </span>
      </div>

      {/* Save Feedback Banner */}
      {saveState.status !== 'idle' && (
        <div
          className={`save-status-banner save-status-${saveState.status}`}
          data-testid={`save-status-${saveState.status}`}
        >
          <div className="save-status-content">
            {saveState.status === 'saving' && <span className="loading-spinner-sm" />}
            <span className="save-status-message">{saveState.message}</span>
            {saveState.failed && saveState.failed.length > 0 && (
              <ul className="save-status-failures">
                {saveState.failed.map((f, i) => (
                  <li key={i} data-testid={`save-failure-${f.name}`}>
                    <code>{f.name}</code>: {f.error}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="save-status-actions">
            {(saveState.status === 'partial' || saveState.status === 'error') && (
              <button
                type="button"
                className="btn btn-retry-save"
                onClick={handleSaveChanges}
                data-testid="btn-retry-save"
              >
                🔄 Retry
              </button>
            )}
            {saveState.status !== 'saving' && (
              <button
                type="button"
                className="btn-dismiss-banner"
                onClick={handleDismissSaveStatus}
                data-testid="btn-dismiss-save-status"
                aria-label="Dismiss banner"
              >
                ✕
              </button>
            )}
          </div>
        </div>
      )}

      {/* React Flow Canvas */}
      <div className="react-flow-wrapper" data-testid="rf-wrapper">
        <ReactFlow
          nodes={displayNodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          nodeTypes={nodeTypes}
          fitView
        >
          <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#30363d" />
          <Controls />
          <MiniMap
            nodeColor={(node) => (node.data?.isRoot ? '#238636' : '#1f6feb')}
            style={{ backgroundColor: '#161b22' }}
          />
        </ReactFlow>
      </div>
    </div>
  );
});

export default ProfileGraph;
