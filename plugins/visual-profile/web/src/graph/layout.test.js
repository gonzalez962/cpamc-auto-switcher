import { describe, it, expect } from 'vitest';
import {
  computeGraphLayout,
  LAYOUT_CONFIG,
  boxesIntersect,
  findBoundingBoxCollisions,
} from './layout';
import { buildGraphFromAuthFiles } from '../api/managementClient';

describe('Layout Geometry & Bounding Box Utilities', () => {
  it('exports documented layout bounds matching CSS card geometry', () => {
    expect(LAYOUT_CONFIG.NODE_WIDTH).toBe(240);
    expect(LAYOUT_CONFIG.NODE_HEIGHT).toBe(240);
    expect(LAYOUT_CONFIG.MIN_NODE_HEIGHT).toBe(150);
    expect(LAYOUT_CONFIG.HORIZONTAL_GAP).toBeGreaterThanOrEqual(50);
    expect(LAYOUT_CONFIG.VERTICAL_GAP).toBeGreaterThanOrEqual(60);
    expect(LAYOUT_CONFIG.LANE_GAP).toBeGreaterThanOrEqual(100);
    expect(LAYOUT_CONFIG.MAX_COLS).toBe(4);
    expect(LAYOUT_CONFIG.MAX_ROW_WIDTH).toBe(1200);
  });

  it('correctly detects intersecting and non-intersecting bounding boxes', () => {
    const boxA = { x: 100, y: 100, width: 240, height: 240 };
    // Overlapping box
    const boxOverlap = { x: 200, y: 150, width: 240, height: 240 };
    expect(boxesIntersect(boxA, boxOverlap)).toBe(true);

    // Adjacent horizontally with gap
    const boxRight = { x: 400, y: 100, width: 240, height: 240 };
    expect(boxesIntersect(boxA, boxRight)).toBe(false);

    // Below vertically with gap (boxA bottom is 100 + 240 = 340)
    const boxBelow = { x: 100, y: 400, width: 240, height: 240 };
    expect(boxesIntersect(boxA, boxBelow)).toBe(false);
  });
});

describe('VP-8 Root/Child Overlap Regression Fix', () => {
  it('positions root strictly above child without overlap when child is at index 0 and root is at index 4', () => {
    // Current live graph bug: buildGraphFromAuthFiles positioned by global idx
    // causing root idx4 y240 and child idx0 y280 to overlap.
    const authFiles = [
      { name: 'child_0.json', prefix: 'agy_p1_1' },  // idx 0: child of agy_p1
      { name: 'child_1.json', prefix: 'agy_p1_2' },  // idx 1: child of agy_p1
      { name: 'isolated_1.json', prefix: 'iso_1' },  // idx 2: isolated
      { name: 'isolated_2.json', prefix: 'iso_2' },  // idx 3: isolated
      { name: 'root.json', prefix: 'agy_p1' },       // idx 4: root of agy_p1
    ];

    const { nodes, edges } = buildGraphFromAuthFiles(authFiles);

    const rootNode = nodes.find((n) => n.id === 'root.json');
    const child0Node = nodes.find((n) => n.id === 'child_0.json');
    const child1Node = nodes.find((n) => n.id === 'child_1.json');

    expect(rootNode).toBeTruthy();
    expect(child0Node).toBeTruthy();
    expect(child1Node).toBeTruthy();

    // Root MUST be strictly above descendants
    expect(rootNode.position.y).toBeLessThan(child0Node.position.y);
    expect(rootNode.position.y).toBeLessThan(child1Node.position.y);

    // Vertical spacing between root and children must be at least card height + gap
    const minVerticalDelta = LAYOUT_CONFIG.NODE_HEIGHT + LAYOUT_CONFIG.VERTICAL_GAP;
    expect(child0Node.position.y - rootNode.position.y).toBeGreaterThanOrEqual(minVerticalDelta);

    // All bounding boxes must be collision-free
    const collisions = findBoundingBoxCollisions(nodes);
    expect(collisions).toHaveLength(0);
  });
});

describe('Deterministic Topology Layout - Large Graphs (20+ Nodes)', () => {
  // Construct a comprehensive test graph with 24 nodes covering:
  // - Multi-level tree (nested chain: root -> child -> grandchild -> great-grandchild)
  // - Branching tree (root -> 2 children, each with 2 grandchildren)
  // - Star tree (root with 5 leaf children)
  // - Ambiguous parent nodes (disconnected in separate lane)
  // - Isolated unlinked nodes (disconnected in separate lane)
  function create24NodeDataset() {
    const nodes = [
      // Tree 1: Deep nested chain (4 levels)
      { id: 'chain_root', data: { prefix: 'chain', isRoot: true } },
      { id: 'chain_c1', data: { prefix: 'chain_1', isRoot: false } },
      { id: 'chain_gc1', data: { prefix: 'chain_1_1', isRoot: false } },
      { id: 'chain_ggc1', data: { prefix: 'chain_1_1_1', isRoot: false } },

      // Tree 2: Branching tree (3 levels)
      { id: 'branch_root', data: { prefix: 'branch', isRoot: true } },
      { id: 'branch_c1', data: { prefix: 'branch_1', isRoot: false } },
      { id: 'branch_c2', data: { prefix: 'branch_2', isRoot: false } },
      { id: 'branch_gc1', data: { prefix: 'branch_1_1', isRoot: false } },
      { id: 'branch_gc2', data: { prefix: 'branch_2_1', isRoot: false } },

      // Tree 3: Wide star tree (root + 5 children)
      { id: 'star_root', data: { prefix: 'star', isRoot: true } },
      { id: 'star_c1', data: { prefix: 'star_1', isRoot: false } },
      { id: 'star_c2', data: { prefix: 'star_2', isRoot: false } },
      { id: 'star_c3', data: { prefix: 'star_3', isRoot: false } },
      { id: 'star_c4', data: { prefix: 'star_4', isRoot: false } },
      { id: 'star_c5', data: { prefix: 'star_5', isRoot: false } },

      // Ambiguous nodes (4 nodes)
      {
        id: 'ambig_1',
        data: {
          prefix: 'shared_1',
          ambiguousParent: { parentPrefix: 'shared', candidateCount: 2 },
        },
      },
      {
        id: 'ambig_2',
        data: {
          prefix: 'shared_2',
          ambiguousParent: { parentPrefix: 'shared', candidateCount: 2 },
        },
      },
      {
        id: 'ambig_3',
        data: {
          prefix: 'pool_1',
          ambiguousParent: { parentPrefix: 'pool', candidateCount: 3 },
        },
      },
      {
        id: 'ambig_4',
        data: {
          prefix: 'pool_2',
          ambiguousParent: { parentPrefix: 'pool', candidateCount: 3 },
        },
      },

      // Isolated / unlinked nodes (5 nodes)
      { id: 'iso_1', data: { prefix: '', label: 'unlinked_1' } },
      { id: 'iso_2', data: { prefix: 'standalone_a', label: 'standalone_a' } },
      { id: 'iso_3', data: { prefix: 'standalone_b', label: 'standalone_b' } },
      { id: 'iso_4', data: { prefix: '', label: 'unlinked_4' } },
      { id: 'iso_5', data: { prefix: 'standalone_c', label: 'standalone_c' } },
    ];

    const edges = [
      // Tree 1 edges
      { id: 'e1', source: 'chain_root', target: 'chain_c1' },
      { id: 'e2', source: 'chain_c1', target: 'chain_gc1' },
      { id: 'e3', source: 'chain_gc1', target: 'chain_ggc1' },

      // Tree 2 edges
      { id: 'e4', source: 'branch_root', target: 'branch_c1' },
      { id: 'e5', source: 'branch_root', target: 'branch_c2' },
      { id: 'e6', source: 'branch_c1', target: 'branch_gc1' },
      { id: 'e7', source: 'branch_c2', target: 'branch_gc2' },

      // Tree 3 edges
      { id: 'e8', source: 'star_root', target: 'star_c1' },
      { id: 'e9', source: 'star_root', target: 'star_c2' },
      { id: 'e10', source: 'star_root', target: 'star_c3' },
      { id: 'e11', source: 'star_root', target: 'star_c4' },
      { id: 'e12', source: 'star_root', target: 'star_c5' },
    ];

    return { nodes, edges };
  }

  it('guarantees zero bounding-box collisions for 20+ nodes at configured card dimensions', () => {
    const { nodes, edges } = create24NodeDataset();
    expect(nodes.length).toBeGreaterThanOrEqual(20);

    const laidOut = computeGraphLayout(nodes, edges);

    const collisions = findBoundingBoxCollisions(laidOut);
    expect(collisions).toHaveLength(0);
  });

  it('arranges root strictly above descendants across all edges, including multi-level chains', () => {
    const { nodes, edges } = create24NodeDataset();
    const laidOut = computeGraphLayout(nodes, edges);
    const posMap = new Map(laidOut.map((n) => [n.id, n.position]));

    // Check all direct edges: source must be strictly above target
    edges.forEach((edge) => {
      const srcPos = posMap.get(edge.source);
      const tgtPos = posMap.get(edge.target);
      expect(srcPos).toBeDefined();
      expect(tgtPos).toBeDefined();
      expect(srcPos.y).toBeLessThan(tgtPos.y);
      expect(tgtPos.y - srcPos.y).toBeGreaterThanOrEqual(
        LAYOUT_CONFIG.NODE_HEIGHT + LAYOUT_CONFIG.VERTICAL_GAP
      );
    });

    // Check 4-level nested chain order
    const chainRoot = posMap.get('chain_root');
    const chainC1 = posMap.get('chain_c1');
    const chainGC1 = posMap.get('chain_gc1');
    const chainGGC1 = posMap.get('chain_ggc1');

    expect(chainRoot.y).toBeLessThan(chainC1.y);
    expect(chainC1.y).toBeLessThan(chainGC1.y);
    expect(chainGC1.y).toBeLessThan(chainGGC1.y);
  });

  it('isolates ambiguous and unlinked nodes in non-overlapping separate lanes', () => {
    const { nodes, edges } = create24NodeDataset();
    const laidOut = computeGraphLayout(nodes, edges);

    const treeNodeIds = new Set([
      'chain_root', 'chain_c1', 'chain_gc1', 'chain_ggc1',
      'branch_root', 'branch_c1', 'branch_c2', 'branch_gc1', 'branch_gc2',
      'star_root', 'star_c1', 'star_c2', 'star_c3', 'star_c4', 'star_c5',
    ]);
    const ambigNodeIds = new Set(['ambig_1', 'ambig_2', 'ambig_3', 'ambig_4']);
    const isoNodeIds = new Set(['iso_1', 'iso_2', 'iso_3', 'iso_4', 'iso_5']);

    const treeNodes = laidOut.filter((n) => treeNodeIds.has(n.id));
    const ambigNodes = laidOut.filter((n) => ambigNodeIds.has(n.id));
    const isoNodes = laidOut.filter((n) => isoNodeIds.has(n.id));

    const maxTreeBottom = Math.max(
      ...treeNodes.map((n) => n.position.y + LAYOUT_CONFIG.NODE_HEIGHT)
    );
    const minAmbigTop = Math.min(...ambigNodes.map((n) => n.position.y));
    const maxAmbigBottom = Math.max(
      ...ambigNodes.map((n) => n.position.y + LAYOUT_CONFIG.NODE_HEIGHT)
    );
    const minIsoTop = Math.min(...isoNodes.map((n) => n.position.y));

    // Ambiguous lane is strictly below all trees with at least LANE_GAP
    expect(minAmbigTop).toBeGreaterThanOrEqual(maxTreeBottom + LAYOUT_CONFIG.LANE_GAP);

    // Isolated lane is strictly below ambiguous lane with at least LANE_GAP
    expect(minIsoTop).toBeGreaterThanOrEqual(maxAmbigBottom + LAYOUT_CONFIG.LANE_GAP);
  });

  it('is strictly deterministic: shuffled input arrays produce identical node positions', () => {
    const { nodes, edges } = create24NodeDataset();

    // Baseline layout
    const baseline = computeGraphLayout(nodes, edges);
    const baselineMap = new Map(baseline.map((n) => [n.id, n.position]));

    // Reverse input array
    const reversedNodes = [...nodes].reverse();
    const laidOutReversed = computeGraphLayout(reversedNodes, edges);

    laidOutReversed.forEach((node) => {
      const basePos = baselineMap.get(node.id);
      expect(node.position.x).toBe(basePos.x);
      expect(node.position.y).toBe(basePos.y);
    });

    // Pseudo-random interleaved permutation
    const shuffledNodes = [
      nodes[3], nodes[15], nodes[0], nodes[20], nodes[8],
      nodes[1], nodes[18], nodes[12], nodes[5], nodes[22],
      nodes[10], nodes[2], nodes[14], nodes[7], nodes[23],
      nodes[9], nodes[16], nodes[4], nodes[19], nodes[11],
      nodes[6], nodes[17], nodes[13], nodes[21],
    ];

    const laidOutShuffled = computeGraphLayout(shuffledNodes, edges);

    laidOutShuffled.forEach((node) => {
      const basePos = baselineMap.get(node.id);
      expect(node.position.x).toBe(basePos.x);
      expect(node.position.y).toBe(basePos.y);
    });
  });

  it('avoids giant infinite horizontal spreading for large graphs by wrapping to rows', () => {
    const { nodes, edges } = create24NodeDataset();
    const laidOut = computeGraphLayout(nodes, edges);

    const minX = Math.min(...laidOut.map((n) => n.position.x));
    const maxX = Math.max(...laidOut.map((n) => n.position.x + LAYOUT_CONFIG.NODE_WIDTH));
    const totalWidth = maxX - minX;

    // Total width across 24 nodes must NOT exceed reasonable bounded canvas width
    expect(totalWidth).toBeLessThanOrEqual(
      LAYOUT_CONFIG.MAX_ROW_WIDTH + LAYOUT_CONFIG.NODE_WIDTH + LAYOUT_CONFIG.HORIZONTAL_GAP
    );
  });

  it('preserves all node business fields, IDs, and edge definitions without side effects', () => {
    const { nodes, edges } = create24NodeDataset();
    const originalNodes = JSON.parse(JSON.stringify(nodes));

    const laidOut = computeGraphLayout(nodes, edges);

    expect(laidOut).toHaveLength(originalNodes.length);
    laidOut.forEach((node) => {
      const original = originalNodes.find((o) => o.id === node.id);
      expect(original).toBeDefined();
      expect(node.data.prefix).toBe(original.data.prefix);
      expect(node.data.label).toBe(original.data.label);
      expect(node.data.isRoot).toBe(original.data.isRoot);
      expect(node.data.ambiguousParent).toEqual(original.data.ambiguousParent);
    });
  });
});

describe('Edge Cases and Flat Graphs', () => {
  it('returns empty array when nodes array is empty', () => {
    expect(computeGraphLayout([], [])).toEqual([]);
    expect(computeGraphLayout(null, null)).toEqual([]);
  });

  it('lays out a graph of only isolated nodes without errors or collisions', () => {
    const isolated = [
      { id: 'f1.json', data: { prefix: '' } },
      { id: 'f2.json', data: { prefix: '' } },
      { id: 'f3.json', data: { prefix: 'iso_1' } },
      { id: 'f4.json', data: { prefix: 'iso_2' } },
      { id: 'f5.json', data: { prefix: 'iso_3' } },
      { id: 'f6.json', data: { prefix: 'iso_4' } },
    ];

    const laidOut = computeGraphLayout(isolated, []);
    expect(laidOut).toHaveLength(6);
    expect(findBoundingBoxCollisions(laidOut)).toHaveLength(0);

    // Verify grid wrapping (max 4 columns)
    const positions = laidOut.map((n) => n.position);
    expect(positions[0].y).toBe(positions[1].y);
    expect(positions[0].y).toBe(positions[2].y);
    expect(positions[0].y).toBe(positions[3].y);
    // 5th node wrapped to next row
    expect(positions[4].y).toBeGreaterThan(positions[0].y);
  });
});

describe('VP-8 Layout Configuration vs CSS Documented Envelope and Sibling Clustering', () => {
  it('verifies LAYOUT_CONFIG matches CSS .profile-node documented envelope', () => {
    // Width matches fixed .profile-node width (240px)
    expect(LAYOUT_CONFIG.NODE_WIDTH).toBe(240);

    // NODE_HEIGHT reserves full maximum safe envelope matching CSS max-height: 240px
    expect(LAYOUT_CONFIG.NODE_HEIGHT).toBe(240);

    // MIN_NODE_HEIGHT documents CSS min-height (150px)
    expect(LAYOUT_CONFIG.MIN_NODE_HEIGHT).toBe(150);

    // Level pitch (NODE_HEIGHT + VERTICAL_GAP = 320px) guarantees at least 80px clearance
    // even under worst-case card expansion (240px), and 134-170px clearance for standard cards (150-186px)
    const levelPitch = LAYOUT_CONFIG.NODE_HEIGHT + LAYOUT_CONFIG.VERTICAL_GAP;
    expect(levelPitch).toBe(320);

    const worstCaseClearance = levelPitch - LAYOUT_CONFIG.NODE_HEIGHT;
    expect(worstCaseClearance).toBeGreaterThanOrEqual(LAYOUT_CONFIG.VERTICAL_GAP);

    const standardCardClearance = levelPitch - 186; // normal card max auto-height
    expect(standardCardClearance).toBeGreaterThanOrEqual(130);
  });

  it('clusters 3 leaf siblings horizontally in a single row without vertical collision', () => {
    const nodes = [
      { id: 'parent.json', data: { prefix: 'agy_p1', isRoot: true } },
      { id: 'c1.json', data: { prefix: 'agy_p1_1', isRoot: false } },
      { id: 'c2.json', data: { prefix: 'agy_p1_2', isRoot: false } },
      { id: 'c3.json', data: { prefix: 'agy_p1_3', isRoot: false } },
    ];
    const edges = [
      { id: 'e1', source: 'parent.json', target: 'c1.json' },
      { id: 'e2', source: 'parent.json', target: 'c2.json' },
      { id: 'e3', source: 'parent.json', target: 'c3.json' },
    ];

    const laidOut = computeGraphLayout(nodes, edges);
    expect(findBoundingBoxCollisions(laidOut)).toHaveLength(0);

    const parent = laidOut.find((n) => n.id === 'parent.json');
    const c1 = laidOut.find((n) => n.id === 'c1.json');
    const c2 = laidOut.find((n) => n.id === 'c2.json');
    const c3 = laidOut.find((n) => n.id === 'c3.json');

    // All siblings are on the same Y row
    expect(c1.position.y).toBe(c2.position.y);
    expect(c2.position.y).toBe(c3.position.y);

    // Level pitch from parent
    expect(c1.position.y - parent.position.y).toBe(
      LAYOUT_CONFIG.NODE_HEIGHT + LAYOUT_CONFIG.VERTICAL_GAP
    );

    // Siblings are sorted and spaced with HORIZONTAL_GAP
    const pitchX = LAYOUT_CONFIG.NODE_WIDTH + LAYOUT_CONFIG.HORIZONTAL_GAP;
    expect(c2.position.x - c1.position.x).toBe(pitchX);
    expect(c3.position.x - c2.position.x).toBe(pitchX);

    // Parent is horizontally centered above the siblings
    const siblingsCenterX = (c1.position.x + c3.position.x + LAYOUT_CONFIG.NODE_WIDTH) / 2;
    const parentCenterX = parent.position.x + LAYOUT_CONFIG.NODE_WIDTH / 2;
    expect(parentCenterX).toBeCloseTo(siblingsCenterX, 0);
  });

  it('keeps up to 5 leaf siblings in a single row to eliminate upper-card edge crossing', () => {
    const nodes = [
      { id: 'parent.json', data: { prefix: 'agy_p1', isRoot: true } },
      { id: 'c1.json', data: { prefix: 'agy_p1_1', isRoot: false } },
      { id: 'c2.json', data: { prefix: 'agy_p1_2', isRoot: false } },
      { id: 'c3.json', data: { prefix: 'agy_p1_3', isRoot: false } },
      { id: 'c4.json', data: { prefix: 'agy_p1_4', isRoot: false } },
      { id: 'c5.json', data: { prefix: 'agy_p1_5', isRoot: false } },
    ];
    const edges = [
      { id: 'e1', source: 'parent.json', target: 'c1.json' },
      { id: 'e2', source: 'parent.json', target: 'c2.json' },
      { id: 'e3', source: 'parent.json', target: 'c3.json' },
      { id: 'e4', source: 'parent.json', target: 'c4.json' },
      { id: 'e5', source: 'parent.json', target: 'c5.json' },
    ];

    const laidOut = computeGraphLayout(nodes, edges);
    expect(findBoundingBoxCollisions(laidOut)).toHaveLength(0);

    const children = laidOut.filter((n) => n.id !== 'parent.json');
    expect(children).toHaveLength(5);

    // All 5 children are in a single horizontal row (identical Y coordinates)
    const firstY = children[0].position.y;
    children.forEach((c) => {
      expect(c.position.y).toBe(firstY);
    });

    // Zero bounding-box collisions
    expect(findBoundingBoxCollisions(laidOut)).toHaveLength(0);
  });

  it('wraps leaf siblings to multiple rows with guaranteed vertical clearance when count exceeds 5', () => {
    // 8 leaf children
    const nodes = [
      { id: 'parent.json', data: { prefix: 'agy_p1', isRoot: true } },
      ...Array.from({ length: 8 }, (_, i) => ({
        id: `c${i + 1}.json`,
        data: { prefix: `agy_p1_${i + 1}`, isRoot: false },
      })),
    ];
    const edges = Array.from({ length: 8 }, (_, i) => ({
      id: `e${i + 1}`,
      source: 'parent.json',
      target: `c${i + 1}.json`,
    }));

    const laidOut = computeGraphLayout(nodes, edges);
    expect(findBoundingBoxCollisions(laidOut)).toHaveLength(0);

    const parent = laidOut.find((n) => n.id === 'parent.json');
    const c1 = laidOut.find((n) => n.id === 'c1.json');
    const c5 = laidOut.find((n) => n.id === 'c5.json'); // wrapped to row 1

    // Row 0 has level pitch from parent
    expect(c1.position.y - parent.position.y).toBe(
      LAYOUT_CONFIG.NODE_HEIGHT + LAYOUT_CONFIG.VERTICAL_GAP
    );

    // Row 1 is separated from Row 0 by level pitch (NODE_HEIGHT + VERTICAL_GAP = 320px)
    expect(c5.position.y - c1.position.y).toBe(
      LAYOUT_CONFIG.NODE_HEIGHT + LAYOUT_CONFIG.VERTICAL_GAP
    );

    // Complete collision check across all 9 nodes
    expect(findBoundingBoxCollisions(laidOut)).toHaveLength(0);
  });
});
