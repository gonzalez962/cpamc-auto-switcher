import { addEdge } from '@xyflow/react';
import {
  calculateChildPrefix,
  countOutgoingEdges,
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
  const parentPrefix =
    sourceNode.data?.prefix || sourceNode.data?.label || sourceNode.id;

  const outgoingCount = countOutgoingEdges(eds, connection.source);
  const nextNumber = outgoingCount + 1;
  const childPrefix = calculateChildPrefix(parentPrefix, nextNumber);

  const newEdge = {
    ...connection,
    id: connection.id || `xy-edge__${connection.source}-${connection.target}`,
    animated: true,
    style: { stroke: '#58a6ff', strokeWidth: 2 },
  };

  const nextEdges = addEdge(newEdge, eds);

  const nextNodes = nds.map((node) => {
    if (node.id === targetNode.id) {
      return {
        ...node,
        data: {
          ...node.data,
          prefix: childPrefix,
          label: childPrefix,
          isRoot: false,
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
        },
      };

      return {
        ...state,
        nodes: [...state.nodes, newNode],
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

    default:
      return state;
  }
}
