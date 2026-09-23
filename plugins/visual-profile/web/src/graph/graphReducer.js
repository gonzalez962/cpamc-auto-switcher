import { addEdge } from '@xyflow/react';
import {
  calculateChildPrefix,
  countOutgoingEdges,
  getNextChildNumber,
  getEdgeId,
  validateConnection,
  getNextAvailableRootPrefix,
  getNextAvailableChildPrefix,
} from './connection';

/**
 * Creates an initial graph state containing nodes and edges.
 *
 * @param {Array<Object>} nodes
 * @param {Array<Object>} edges
 * @returns {{ nodes: Array<Object>, edges: Array<Object> }}
 */
export function createInitialGraphState(nodes = [], edges = []) {
  return {
    nodes: Array.isArray(nodes) ? nodes : [],
    edges: Array.isArray(edges) ? edges : [],
  };
}

/**
 * Connects a parent node to a child node atomically.
 * Pure reducer helper:
 * - Parent prefix is derived directly from current nds.
 * - Outgoing count is derived directly from current eds.
 * - Redundant mutable outgoingCount is removed from node.data (derived dynamically at view layer).
 * - Child's prefix, label, and dirty state are updated atomically.
 * - If connection is invalid (missing source/target, self-connection) or duplicate
 *   or multi-parent (target already has parent), state is returned unchanged without renaming the child.
 *
 * @param {{ nodes: Array<Object>, edges: Array<Object> }} state
 * @param {Object} connection
 * @returns {{ nodes: Array<Object>, edges: Array<Object> }}
 */
export function connectNodesAtomic(state, connection) {
  const { nodes: nds, edges: eds } = state;

  const validation = validateConnection(nds, eds, connection);
  if (!validation.valid) {
    // Guard: invalid, duplicate, or multi-parent connections must NOT rename child or mutate state
    return state;
  }

  const { sourceNode, targetNode } = validation;
  const parentPrefix = String(sourceNode.data?.prefix || '').trim();
  if (!parentPrefix) {
    return state;
  }

  const nextNumber = getNextChildNumber(nds, eds, connection.source, parentPrefix);
  const childPrefix = calculateChildPrefix(parentPrefix, nextNumber);

  const newEdge = {
    ...connection,
    id: connection.id || getEdgeId(connection.source, connection.target),
    animated: true,
    style: { stroke: '#58a6ff', strokeWidth: 2 },
  };

  const nextEdges = addEdge(newEdge, eds);

  const nextNodes = nds.map((node) => {
    if (node.id === targetNode.id) {
      const initial =
        node.data?.initialPrefix !== undefined ? node.data.initialPrefix : null;
      const isDirty = initial !== null ? childPrefix !== initial : true;
      return {
        ...node,
        data: {
          ...node.data,
          prefix: childPrefix,
          label: childPrefix,
          isRoot: false,
          isDirty,
        },
      };
    }
    return node;
  });

  return {
    nodes: nextNodes,
    edges: nextEdges,
  };
}

/**
 * Predictable atomic graph reducer owning both nodes and edges.
 * Strictly pure: no side effects, no Date.now(), no mutable module state.
 * Fully idempotent under React StrictMode double-invocations.
 *
 * @param {{ nodes: Array<Object>, edges: Array<Object> }} state
 * @param {Object} action
 * @returns {{ nodes: Array<Object>, edges: Array<Object> }}
 */
export function graphReducer(state, action) {
  switch (action.type) {
    case 'CONNECT': {
      return connectNodesAtomic(state, action.connection);
    }

    case 'UPDATE_NODE_PREFIX': {
      const targetId = action.id;
      const rawPrefix = typeof action.prefix === 'string' ? action.prefix : '';
      const trimmed = rawPrefix.trim();

      // Validate: empty string is allowed per API ("exact allowed empty per API").
      // Non-empty prefix must be valid alphanumeric/underscore/dash.
      const isValid = trimmed === '' || /^[a-zA-Z0-9_-]+$/.test(trimmed);
      if (!isValid) {
        return state;
      }

      const targetNode = state.nodes.find((n) => n.id === targetId);
      if (!targetNode) {
        return state;
      }

      const currentPrefix =
        targetNode.data?.prefix !== undefined ? targetNode.data.prefix : '';
      if (trimmed === currentPrefix) {
        return state;
      }

      // 1. Parent attached children check: reject parent edit while attached children exist
      // to maintain graph/prefix coherence and avoid stale descendant prefixes
      const hasChildren = state.edges.some((e) => e && e.source === targetId);
      if (hasChildren) {
        return state;
      }

      // 2. Child manual edit: detach incoming edge to prevent false parent edge
      const incomingEdge = state.edges.find((e) => e && e.target === targetId);
      let nextEdges = state.edges;
      if (incomingEdge) {
        nextEdges = state.edges.filter((e) => !e || e.target !== targetId);
      }

      const nextNodes = state.nodes.map((node) => {
        if (node.id === targetId) {
          const initial =
            node.data?.initialPrefix !== undefined ? node.data.initialPrefix : '';
          const isDirty = trimmed !== initial;
          const label = trimmed || node.data?.fileName || node.id;
          const hasParentAfterEdit = nextEdges.some((e) => e && e.target === node.id);
          const isRoot = !hasParentAfterEdit && Boolean(trimmed);
          return {
            ...node,
            data: {
              ...node.data,
              prefix: trimmed,
              label,
              isDirty,
              isRoot,
            },
          };
        }
        return node;
      });

      return {
        ...state,
        nodes: nextNodes,
        edges: nextEdges,
      };
    }

    case 'ADD_ROOT_NODE': {
      // action.id MUST be generated before dispatch (in event handler) to preserve reducer purity
      const newId = action.id || `p_${state.nodes.length + 1}`;
      const customPrefix = action.customPrefix?.trim();
      const prefix = customPrefix || getNextAvailableRootPrefix(state.nodes);
      const idx = state.nodes.length;
      const position = action.position || {
        x: 100 + (idx % 5) * 160,
        y: 60 + Math.floor(idx / 5) * 140,
      };

      const newNode = {
        id: newId,
        type: 'profile',
        position,
        data: {
          label: prefix,
          prefix,
          isRoot: true,
          isSynthetic: true, // Marked synthetic so it cannot be persisted
          isDirty: false,
        },
      };

      return {
        ...state,
        nodes: [...state.nodes, newNode],
      };
    }

    case 'ADD_CHILD_NODE': {
      // action.id MUST be generated before dispatch (in event handler) to preserve reducer purity
      const newId = action.id || `c_${state.nodes.length + 1}`;
      const label = getNextAvailableChildPrefix(state.nodes);
      const idx = state.nodes.length;
      const position = action.position || {
        x: 120 + (idx % 5) * 160,
        y: 280 + Math.floor(idx / 5) * 120,
      };

      const newNode = {
        id: newId,
        type: 'profile',
        position,
        data: {
          label,
          prefix: label,
          isRoot: false,
          isSynthetic: true, // Marked synthetic so it cannot be persisted
          isDirty: false,
        },
      };

      return {
        ...state,
        nodes: [...state.nodes, newNode],
      };
    }

    case 'LOAD_GRAPH': {
      return {
        nodes: Array.isArray(action.nodes) ? action.nodes : [],
        edges: Array.isArray(action.edges) ? action.edges : [],
      };
    }

    case 'SET_NODES': {
      const nextNodes =
        typeof action.updater === 'function'
          ? action.updater(state.nodes)
          : action.updater;
      return {
        ...state,
        nodes: Array.isArray(nextNodes) ? nextNodes : state.nodes,
      };
    }

    case 'SET_EDGES': {
      const nextEdges =
        typeof action.updater === 'function'
          ? action.updater(state.edges)
          : action.updater;
      return {
        ...state,
        edges: Array.isArray(nextEdges) ? nextEdges : state.edges,
      };
    }

    case 'RESET': {
      return createInitialGraphState(action.nodes, action.edges);
    }

    case 'RECONCILE_SAVED_NODES': {
      const successfulMap = new Map();
      if (Array.isArray(action.successful)) {
        action.successful.forEach((item) => {
          if (item && typeof item.name === 'string') {
            successfulMap.set(
              item.name,
              item.prefix !== undefined ? String(item.prefix).trim() : ''
            );
          }
        });
      }

      const failedSet = new Set();
      if (Array.isArray(action.failed)) {
        action.failed.forEach((item) => {
          if (item && typeof item.name === 'string') {
            failedSet.add(item.name);
          }
        });
      }

      const nextNodes = state.nodes.map((node) => {
        const fileKey = node.data?.fileName || node.id;

        if (successfulMap.has(fileKey)) {
          const submittedPrefix = successfulMap.get(fileKey);
          // Advance initialPrefix to the submitted value that was successfully saved
          // Preserve the latest draft prefix on the node (which may have been edited while save was pending)
          const currentPrefix =
            node.data?.prefix !== undefined ? String(node.data.prefix).trim() : '';
          const isDirty = currentPrefix !== submittedPrefix;

          return {
            ...node,
            data: {
              ...node.data,
              initialPrefix: submittedPrefix,
              isDirty,
            },
          };
        }

        if (failedSet.has(fileKey)) {
          // Failed files retain their original initialPrefix and remain dirty
          return {
            ...node,
            data: {
              ...node.data,
              isDirty: true,
            },
          };
        }

        return node;
      });

      return {
        ...state,
        nodes: nextNodes,
      };
    }

    default:
      return state;
  }
}
