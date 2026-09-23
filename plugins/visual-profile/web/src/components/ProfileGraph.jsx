import React, {
  useState,
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

  // Expose imperative handle for direct programmatic testing
  useImperativeHandle(
    ref,
    () => ({
      connect: (conn) => onConnect(conn),
      onConnect,
      getNodes: () => nodes,
      getEdges: () => edges,
      reset: () => handleResetGraph(),
    }),
    [onConnect, nodes, edges]
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

  /**
   * Resets graph restoring initial props (safeInitialNodes, safeInitialEdges).
   */
  const handleResetGraph = useCallback(() => {
    dispatch({ type: 'RESET', nodes: safeInitialNodes, edges: safeInitialEdges });
  }, [safeInitialNodes, safeInitialEdges]);

  // Derive outgoingCount for nodes directly from current eds (no stale redundant counts)
  const displayNodes = useMemo(() => {
    return nodes.map((node) => {
      const derivedCount = countOutgoingEdges(edges, node.id);
      if (node.data?.outgoingCount === derivedCount) {
        return node;
      }
      return {
        ...node,
        data: {
          ...node.data,
          outgoingCount: derivedCount,
        },
      };
    });
  }, [nodes, edges]);

  // Derived metrics for UI status bar
  const rootCount = useMemo(() => nodes.filter((n) => n.data?.isRoot).length, [nodes]);
  const childCount = useMemo(() => nodes.filter((n) => !n.data?.isRoot).length, [nodes]);

  return (
    <div className="graph-container">
      {/* Top Banner and Toolbar */}
      <div className="graph-toolbar">
        <div className="toolbar-left">
          <span className="toolbar-title">Visual Profile Prefix Topology</span>
          <span className="badge-editor-only">Editor Only — Local Graph</span>
        </div>

        <div className="toolbar-controls">
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

          <button
            type="button"
            onClick={handleResetGraph}
            className="btn btn-outline"
            data-testid="btn-reset-graph"
          >
            Reset Graph
          </button>
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
        <span className="stats-divider">•</span>
        <span className="stats-hint">
          Connect a parent node to a child node to assign <code>{'{parentPrefix}_{index}'}</code>
        </span>
      </div>

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
