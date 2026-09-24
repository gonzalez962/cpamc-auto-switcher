/**
 * Deterministic topology-aware layout for CLIProxyAPI Visual Profile graph.
 *
 * Requirements:
 * - Each connected tree arranged with root above descendants (multi-level supported).
 * - Isolated and ambiguous nodes arranged in non-overlapping separate lanes.
 * - Collision-free bounding boxes for all nodes even with 20+ nodes and interleaved ordering.
 * - Avoids giant infinite horizontal spreading with bounded rows and columns.
 * - Uses constants shared with CSS geometry (card width: 240px, safe height bound: 240px).
 * - Honest edge-crossing invariant: Layout strictly eliminates card-on-card bounding box collisions.
 *   Standard React Flow edges to multi-row descendants or across dense branches may visually pass
 *   across canvas lanes or upper sibling cards; node collision defense is the guaranteed invariant.
 * - 100% pure, deterministic, zero external network/CDN dependencies.
 */

export const LAYOUT_CONFIG = {
  NODE_WIDTH: 240,       // Fixed card width matching CSS .profile-node (width / max-width: 240px)
  NODE_HEIGHT: 240,      // Safe maximum bounding box height matching CSS max-height: 240px
  MIN_NODE_HEIGHT: 150,  // Documented minimum card height matching CSS min-height: 150px
  HORIZONTAL_GAP: 60,    // Gap between adjacent nodes or subtrees in X
  VERTICAL_GAP: 80,      // Gap between rows/levels in Y
  LANE_GAP: 120,         // Gap between separate lanes (Trees, Ambiguous, Isolated) in Y
  START_X: 80,           // Margin on canvas
  START_Y: 80,           // Margin on canvas
  MAX_COLS: 4,           // Max columns in grid lanes
  MAX_ROW_WIDTH: 1200,   // Max horizontal width before wrapping component trees
};

/**
 * Checks whether two axis-aligned bounding boxes intersect.
 *
 * @param {{ x: number, y: number, width: number, height: number }} boxA
 * @param {{ x: number, y: number, width: number, height: number }} boxB
 * @returns {boolean} True if the boxes intersect
 */
export function boxesIntersect(boxA, boxB) {
  return (
    boxA.x < boxB.x + boxB.width &&
    boxA.x + boxA.width > boxB.x &&
    boxA.y < boxB.y + boxB.height &&
    boxA.y + boxA.height > boxB.y
  );
}

/**
 * Finds all pairs of nodes with overlapping bounding boxes.
 * Useful for automated validation and unit testing.
 *
 * @param {Array<Object>} nodes
 * @param {Object} [config]
 * @returns {Array<{ a: string, b: string, boxA: Object, boxB: Object }>}
 */
export function findBoundingBoxCollisions(nodes, config = LAYOUT_CONFIG) {
  const collisions = [];
  const width = config.NODE_WIDTH;
  const height = config.NODE_HEIGHT;
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i];
      const b = nodes[j];
      if (!a.position || !b.position) continue;
      const boxA = { x: a.position.x, y: a.position.y, width, height };
      const boxB = { x: b.position.x, y: b.position.y, width, height };
      if (boxesIntersect(boxA, boxB)) {
        collisions.push({ a: a.id, b: b.id, boxA, boxB });
      }
    }
  }
  return collisions;
}

/**
 * Computes deterministic, topology-aware, collision-free initial layout for visual profile nodes.
 *
 * Topology rules:
 * 1. Connected components (trees/DAGs): Each tree has its root(s) placed strictly above descendants.
 *    Multi-level nested chains (root -> child -> grandchild) are positioned hierarchically.
 *    Multiple trees wrap into reasonable rows to prevent infinite horizontal spreading.
 * 2. Ambiguous nodes: Nodes flagged with ambiguousParent are placed in their own separate lane below trees.
 * 3. Isolated nodes: Independent nodes without edges or ambiguity are placed in their own separate lane.
 * 4. Determinism: Nodes and components are ordered lexicographically by immutable ID, guaranteeing
 *    identical positions regardless of input array interleaving.
 *
 * @param {Array<Object>} nodes
 * @param {Array<Object>} edges
 * @param {Object} [customConfig]
 * @returns {Array<Object>} Nodes with updated position coordinates { x, y }
 */
export function computeGraphLayout(nodes = [], edges = [], customConfig = {}) {
  const config = { ...LAYOUT_CONFIG, ...customConfig };
  if (!Array.isArray(nodes) || nodes.length === 0) {
    return [];
  }

  const nodeMap = new Map();
  nodes.forEach((n) => nodeMap.set(n.id, n));

  const validEdges = (Array.isArray(edges) ? edges : []).filter(
    (e) => e && nodeMap.has(e.source) && nodeMap.has(e.target) && e.source !== e.target
  );

  const outgoing = new Map();
  const incoming = new Map();
  nodes.forEach((n) => {
    outgoing.set(n.id, []);
    incoming.set(n.id, []);
  });
  validEdges.forEach((e) => {
    outgoing.get(e.source).push(e.target);
    incoming.get(e.target).push(e.source);
  });

  // Categorize nodes
  const connectedNodeIds = new Set();
  validEdges.forEach((e) => {
    connectedNodeIds.add(e.source);
    connectedNodeIds.add(e.target);
  });

  const connectedNodes = [];
  const ambiguousNodes = [];
  const isolatedNodes = [];

  nodes.forEach((node) => {
    if (connectedNodeIds.has(node.id)) {
      connectedNodes.push(node);
    } else if (node.data?.ambiguousParent) {
      ambiguousNodes.push(node);
    } else {
      isolatedNodes.push(node);
    }
  });

  // Group connected nodes into weakly connected components
  const undirected = new Map();
  connectedNodes.forEach((n) => undirected.set(n.id, []));
  validEdges.forEach((e) => {
    if (undirected.has(e.source) && undirected.has(e.target)) {
      undirected.get(e.source).push(e.target);
      undirected.get(e.target).push(e.source);
    }
  });

  const visited = new Set();
  const rawComponents = [];
  const sortedConnected = [...connectedNodes].sort((a, b) => a.id.localeCompare(b.id));

  sortedConnected.forEach((node) => {
    if (!visited.has(node.id)) {
      const compIds = [];
      const q = [node.id];
      visited.add(node.id);
      while (q.length > 0) {
        const curr = q.shift();
        compIds.push(curr);
        (undirected.get(curr) || []).forEach((nbr) => {
          if (!visited.has(nbr)) {
            visited.add(nbr);
            q.push(nbr);
          }
        });
      }
      compIds.sort((a, b) => a.localeCompare(b));
      const compIdSet = new Set(compIds);
      const compEdges = validEdges.filter(
        (e) => compIdSet.has(e.source) && compIdSet.has(e.target)
      );
      rawComponents.push({ nodeIds: compIds, edges: compEdges, idSet: compIdSet });
    }
  });

  // Lay out each component
  const laidOutComponents = rawComponents.map((comp) => {
    const { nodeIds, idSet } = comp;
    let roots = nodeIds.filter(
      (id) => incoming.get(id).filter((s) => idSet.has(s)).length === 0
    );
    if (roots.length === 0) {
      roots = [nodeIds[0]];
    }
    roots.sort((a, b) => a.localeCompare(b));

    const visitedInComp = new Set();

    function layoutSubtree(nodeId) {
      visitedInComp.add(nodeId);
      const children = (outgoing.get(nodeId) || [])
        .filter((target) => idSet.has(target) && !visitedInComp.has(target))
        .sort((a, b) => a.localeCompare(b));

      if (children.length === 0) {
        return {
          width: config.NODE_WIDTH,
          height: config.NODE_HEIGHT,
          positions: new Map([[nodeId, { relX: 0, relY: 0 }]]),
        };
      }

      // Check if all children are leaf nodes
      const allChildrenLeaves = children.every(
        (c) =>
          (outgoing.get(c) || []).filter(
            (t) => idSet.has(t) && !visitedInComp.has(t)
          ).length === 0
      );

      if (allChildrenLeaves && children.length > 1) {
        // Keep leaf siblings in a single row when they fit within horizontal capacity (up to 5 siblings, <= 1440px),
        // reducing multi-row edge crossings between root and lower-row descendants.
        // Wrap to compact grid rows when sibling count exceeds horizontal capacity (> 5).
        const canFitSingleRow =
          children.length <= 5 &&
          children.length * config.NODE_WIDTH + (children.length - 1) * config.HORIZONTAL_GAP <=
            config.MAX_ROW_WIDTH + config.NODE_WIDTH;
        const maxCols = canFitSingleRow
          ? children.length
          : Math.min(children.length, config.MAX_COLS);
        const numRows = Math.ceil(children.length / maxCols);
        const childrenWidth =
          maxCols * config.NODE_WIDTH + (maxCols - 1) * config.HORIZONTAL_GAP;
        const totalWidth = Math.max(config.NODE_WIDTH, childrenWidth);
        const rootRelX = (totalWidth - config.NODE_WIDTH) / 2;
        const childrenOffsetX = (totalWidth - childrenWidth) / 2;

        const subPositions = new Map();
        subPositions.set(nodeId, { relX: rootRelX, relY: 0 });

        const levelPitch = config.NODE_HEIGHT + config.VERTICAL_GAP;

        children.forEach((cId, idx) => {
          visitedInComp.add(cId);
          const col = idx % maxCols;
          const row = Math.floor(idx / maxCols);
          const cX = childrenOffsetX + col * (config.NODE_WIDTH + config.HORIZONTAL_GAP);
          const cY = levelPitch + row * (config.NODE_HEIGHT + config.VERTICAL_GAP);
          subPositions.set(cId, { relX: cX, relY: cY });
        });

        const totalHeight =
          levelPitch +
          (numRows - 1) * (config.NODE_HEIGHT + config.VERTICAL_GAP) +
          config.NODE_HEIGHT;

        return {
          width: totalWidth,
          height: totalHeight,
          positions: subPositions,
        };
      }

      // General case: children may have their own subtrees
      const childLayouts = children.map((c) => layoutSubtree(c));

      let totalChildrenWidth = 0;
      childLayouts.forEach((cl, i) => {
        totalChildrenWidth += cl.width;
        if (i < childLayouts.length - 1) {
          totalChildrenWidth += config.HORIZONTAL_GAP;
        }
      });

      const totalWidth = Math.max(config.NODE_WIDTH, totalChildrenWidth);
      const childrenOffsetX = (totalWidth - totalChildrenWidth) / 2;
      const rootRelX = (totalWidth - config.NODE_WIDTH) / 2;

      const subPositions = new Map();
      subPositions.set(nodeId, { relX: rootRelX, relY: 0 });

      const levelPitch = config.NODE_HEIGHT + config.VERTICAL_GAP;
      let curX = childrenOffsetX;
      let maxChildHeight = 0;

      childLayouts.forEach((cl) => {
        cl.positions.forEach((pos, id) => {
          subPositions.set(id, {
            relX: curX + pos.relX,
            relY: levelPitch + pos.relY,
          });
        });
        maxChildHeight = Math.max(maxChildHeight, cl.height);
        curX += cl.width + config.HORIZONTAL_GAP;
      });

      const totalHeight = levelPitch + maxChildHeight;

      return {
        width: totalWidth,
        height: totalHeight,
        positions: subPositions,
      };
    }

    const rootLayouts = roots.map((r) => layoutSubtree(r));
    let compTotalWidth = 0;
    rootLayouts.forEach((rl, i) => {
      compTotalWidth += rl.width;
      if (i < rootLayouts.length - 1) {
        compTotalWidth += config.HORIZONTAL_GAP;
      }
    });

    let curRootX = 0;
    let maxCompHeight = 0;
    const positions = new Map();

    rootLayouts.forEach((rl) => {
      rl.positions.forEach((pos, id) => {
        positions.set(id, {
          relX: curRootX + pos.relX,
          relY: pos.relY,
        });
      });
      maxCompHeight = Math.max(maxCompHeight, rl.height);
      curRootX += rl.width + config.HORIZONTAL_GAP;
    });

    // Handle any nodes in component not reached by root traversal (defensive)
    nodeIds.forEach((id) => {
      if (!positions.has(id)) {
        positions.set(id, {
          relX: compTotalWidth + config.HORIZONTAL_GAP,
          relY: 0,
        });
        compTotalWidth += config.NODE_WIDTH + config.HORIZONTAL_GAP;
      }
    });

    return {
      primaryRootId: roots[0],
      width: compTotalWidth,
      height: maxCompHeight,
      positions,
    };
  });

  // Sort components deterministically by primary root ID
  laidOutComponents.sort((a, b) => a.primaryRootId.localeCompare(b.primaryRootId));

  const finalPositions = new Map();

  // Lane 1: Connected Component Trees
  let canvasX = config.START_X;
  let canvasY = config.START_Y;
  let rowMaxHeight = 0;
  let rowItemCount = 0;

  laidOutComponents.forEach((comp) => {
    if (rowItemCount > 0 && canvasX + comp.width > config.START_X + config.MAX_ROW_WIDTH) {
      canvasX = config.START_X;
      canvasY += rowMaxHeight + config.VERTICAL_GAP;
      rowMaxHeight = 0;
      rowItemCount = 0;
    }

    comp.positions.forEach((pos, id) => {
      finalPositions.set(id, {
        x: canvasX + pos.relX,
        y: canvasY + pos.relY,
      });
    });

    canvasX += comp.width + config.HORIZONTAL_GAP;
    rowMaxHeight = Math.max(rowMaxHeight, comp.height);
    rowItemCount++;
  });

  const treesBottomY =
    laidOutComponents.length > 0 ? canvasY + rowMaxHeight : config.START_Y;

  // Lane 2: Ambiguous nodes
  ambiguousNodes.sort((a, b) => a.id.localeCompare(b.id));
  let ambiguousBottomY = treesBottomY;

  if (ambiguousNodes.length > 0) {
    const ambiguousStartY =
      laidOutComponents.length > 0 ? treesBottomY + config.LANE_GAP : config.START_Y;
    const cols = Math.min(ambiguousNodes.length, config.MAX_COLS);

    ambiguousNodes.forEach((node, idx) => {
      const col = idx % cols;
      const row = Math.floor(idx / cols);
      finalPositions.set(node.id, {
        x: config.START_X + col * (config.NODE_WIDTH + config.HORIZONTAL_GAP),
        y: ambiguousStartY + row * (config.NODE_HEIGHT + config.VERTICAL_GAP),
      });
    });

    const numRows = Math.ceil(ambiguousNodes.length / cols);
    ambiguousBottomY =
      ambiguousStartY +
      (numRows - 1) * (config.NODE_HEIGHT + config.VERTICAL_GAP) +
      config.NODE_HEIGHT;
  }

  // Lane 3: Isolated nodes
  isolatedNodes.sort((a, b) => a.id.localeCompare(b.id));

  if (isolatedNodes.length > 0) {
    let isolatedStartY = config.START_Y;
    if (ambiguousNodes.length > 0) {
      isolatedStartY = ambiguousBottomY + config.LANE_GAP;
    } else if (laidOutComponents.length > 0) {
      isolatedStartY = treesBottomY + config.LANE_GAP;
    }

    const cols = Math.min(isolatedNodes.length, config.MAX_COLS);

    isolatedNodes.forEach((node, idx) => {
      const col = idx % cols;
      const row = Math.floor(idx / cols);
      finalPositions.set(node.id, {
        x: config.START_X + col * (config.NODE_WIDTH + config.HORIZONTAL_GAP),
        y: isolatedStartY + row * (config.NODE_HEIGHT + config.VERTICAL_GAP),
      });
    });
  }

  // Return nodes with updated positions
  return nodes.map((node) => ({
    ...node,
    position: finalPositions.get(node.id) || node.position || {
      x: config.START_X,
      y: config.START_Y,
    },
  }));
}
