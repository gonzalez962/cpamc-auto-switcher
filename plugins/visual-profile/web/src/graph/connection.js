/**
 * Pure functions for visual profile graph topology and prefix calculations.
 */

/**
 * Calculates the child prefix given a parent prefix and the next sequence number.
 * Preserves the exact parent prefix with all underscores and special characters.
 *
 * @param {string} parentPrefix - The exact parent prefix (e.g., 'agy_p1', 'agy_p2')
 * @param {number} nextNumber - The 1-based child sequence number
 * @returns {string} The formatted child prefix `${parentPrefix}_${nextNumber}`
 */
export function calculateChildPrefix(parentPrefix, nextNumber) {
  const safePrefix =
    parentPrefix != null && String(parentPrefix).trim() !== ''
      ? String(parentPrefix).trim()
      : 'profile';
  const safeNumber = Number.isInteger(nextNumber) && nextNumber > 0 ? nextNumber : 1;
  return `${safePrefix}_${safeNumber}`;
}

/**
 * Counts the exact number of outgoing edges originating from a source node in the current edges list.
 *
 * @param {Array<Object>} edges - The current array of edges
 * @param {string} sourceId - The ID of the source (parent) node
 * @returns {number} Exact count of outgoing edges from sourceId
 */
export function countOutgoingEdges(edges, sourceId) {
  if (!Array.isArray(edges) || !sourceId) {
    return 0;
  }
  return edges.filter((e) => e && e.source === sourceId).length;
}

/**
 * Canonical edge identifier generator.
 * Enforces unambiguous deterministic edge identity across graphs.
 *
 * @param {string} source
 * @param {string} target
 * @returns {string} Deterministic edge ID `edge__${source}-${target}`
 */
export function getEdgeId(source, target) {
  return `edge__${source}-${target}`;
}

/**
 * Determines the next 1-based child sequence number for a given source node.
 * Avoids collisions with existing child prefixes across all nodes when edges are deleted
 * or when custom child prefixes exist, while preserving count + 1 when available.
 *
 * Supports both signatures:
 * - getNextChildNumber(edges, sourceId) [legacy fallback]
 * - getNextChildNumber(nodes, edges, sourceId, parentPrefix) [collision-free]
 *
 * @param {Array<Object>} nodesOrEdges
 * @param {Array<Object>|string} edgesOrSourceId
 * @param {string} [sourceIdOrParentPrefix]
 * @param {string} [parentPrefix]
 * @returns {number} The next sequence number
 */
export function getNextChildNumber(
  nodesOrEdges,
  edgesOrSourceId,
  sourceIdOrParentPrefix,
  parentPrefix
) {
  if (Array.isArray(nodesOrEdges) && typeof edgesOrSourceId === 'string') {
    // Legacy signature: (edges, sourceId)
    return countOutgoingEdges(nodesOrEdges, edgesOrSourceId) + 1;
  }

  const nodes = nodesOrEdges;
  const edges = edgesOrSourceId;
  const sourceId = sourceIdOrParentPrefix;
  const pfx = parentPrefix;

  const baseCount = countOutgoingEdges(edges, sourceId) + 1;
  if (!pfx || !Array.isArray(nodes)) {
    return baseCount;
  }

  const existingPrefixes = new Set(
    nodes
      .map((n) => (n && n.data ? n.data.prefix : ''))
      .filter(Boolean)
  );

  let candidate = baseCount;
  while (existingPrefixes.has(`${pfx}_${candidate}`)) {
    candidate++;
  }
  return candidate;
}

/**
 * Determines the next available root profile prefix (e.g., 'agy_p1', 'agy_p2')
 * avoiding collisions with existing node prefixes.
 *
 * @param {Array<Object>} nodes - Current array of nodes
 * @returns {string} The next available prefix 'agy_p{N}'
 */
export function getNextAvailableRootPrefix(nodes = []) {
  const existing = new Set(
    (Array.isArray(nodes) ? nodes : [])
      .map((n) => n?.data?.prefix || n?.data?.label)
      .filter(Boolean)
  );

  let i = 1;
  while (existing.has(`agy_p${i}`)) {
    i++;
  }
  return `agy_p${i}`;
}

/**
 * Determines the next available child profile prefix (e.g., 'unlinked_1', 'unlinked_2')
 * avoiding collisions with existing node prefixes.
 *
 * @param {Array<Object>} nodes - Current array of nodes
 * @returns {string} The next available prefix 'unlinked_{N}'
 */
export function getNextAvailableChildPrefix(nodes = []) {
  const existing = new Set(
    (Array.isArray(nodes) ? nodes : [])
      .map((n) => n?.data?.prefix || n?.data?.label)
      .filter(Boolean)
  );

  let i = 1;
  while (existing.has(`unlinked_${i}`)) {
    i++;
  }
  return `unlinked_${i}`;
}

/**
 * Validates whether a connection can be established between source and target nodes.
 * Enforces single-parent topology: a target child cannot have multiple parents.
 * Duplicate edges and multi-parent connections are rejected.
 *
 * @param {Array<Object>} nodes - Current array of nodes
 * @param {Array<Object>} edges - Current array of edges
 * @param {Object} connection - Proposed connection with source and target IDs
 * @returns {{ valid: boolean, reason?: string, sourceNode?: Object, targetNode?: Object }}
 */
export function validateConnection(nodes, edges, connection) {
  if (!connection || typeof connection !== 'object') {
    return { valid: false, reason: 'invalid_connection_object' };
  }
  const { source, target } = connection;
  if (!source || !target) {
    return { valid: false, reason: 'missing_endpoints' };
  }
  if (source === target) {
    return { valid: false, reason: 'self_connection' };
  }

  const nodeList = Array.isArray(nodes) ? nodes : [];
  const edgeList = Array.isArray(edges) ? edges : [];

  const sourceNode = nodeList.find((n) => n && n.id === source);
  const targetNode = nodeList.find((n) => n && n.id === target);

  if (!sourceNode) {
    return { valid: false, reason: 'source_not_found' };
  }
  if (!targetNode) {
    return { valid: false, reason: 'target_not_found' };
  }

  // Reject duplicate connection between same source and target
  const alreadyConnected = edgeList.some(
    (e) => e && e.source === source && e.target === target
  );
  if (alreadyConnected) {
    return { valid: false, reason: 'already_connected', sourceNode, targetNode };
  }

  // Reject multi-parent connection: child can only belong to one parent prefix pool
  const hasIncomingEdge = edgeList.some((e) => e && e.target === target);
  if (hasIncomingEdge) {
    return { valid: false, reason: 'target_already_has_parent', sourceNode, targetNode };
  }

  // Source node MUST have a non-empty prefix to establish child routes
  // Prevents falling back to filename or assigning invalid child routes
  const sourcePrefix =
    sourceNode.data?.prefix !== undefined ? String(sourceNode.data.prefix).trim() : '';
  if (!sourcePrefix) {
    return { valid: false, reason: 'source_prefix_empty', sourceNode, targetNode };
  }

  return { valid: true, sourceNode, targetNode };
}

/**
 * Pure state transformation to apply a connection across nodes and edges.
 * Does not mutate input arrays or objects.
 *
 * @param {Array<Object>} nodes
 * @param {Array<Object>} edges
 * @param {Object} connection
 * @returns {{ nodes: Array<Object>, edges: Array<Object>, childPrefix: string | null, applied: boolean }}
 */
export function applyConnectionPure(nodes, edges, connection) {
  const validation = validateConnection(nodes, edges, connection);
  if (!validation.valid) {
    return {
      nodes,
      edges,
      childPrefix: null,
      applied: false,
      reason: validation.reason,
    };
  }

  const { sourceNode, targetNode } = validation;
  const parentPrefix = String(sourceNode.data.prefix).trim();
  const nextNumber = getNextChildNumber(
    nodes,
    edges,
    connection.source,
    parentPrefix
  );
  const childPrefix = calculateChildPrefix(parentPrefix, nextNumber);

  const newEdge = {
    ...connection,
    id: connection.id || getEdgeId(connection.source, connection.target),
  };

  const updatedEdges = [...edges, newEdge];
  const updatedNodes = nodes.map((node) => {
    if (node.id === targetNode.id) {
      return {
        ...node,
        data: {
          ...node.data,
          prefix: childPrefix,
          label: childPrefix,
        },
      };
    }
    return node;
  });

  return {
    nodes: updatedNodes,
    edges: updatedEdges,
    childPrefix,
    applied: true,
  };
}
