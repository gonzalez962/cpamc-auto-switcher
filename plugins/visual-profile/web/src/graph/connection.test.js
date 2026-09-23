import { describe, it, expect } from 'vitest';
import {
  calculateChildPrefix,
  countOutgoingEdges,
  getNextChildNumber,
  getEdgeId,
  validateConnection,
  applyConnectionPure,
  canDisconnectTarget,
  partitionEdgeRemovals,
} from './connection';

describe('calculateChildPrefix', () => {
  it('assigns child prefix for agy_p1 correctly', () => {
    expect(calculateChildPrefix('agy_p1', 1)).toBe('agy_p1_1');
    expect(calculateChildPrefix('agy_p1', 2)).toBe('agy_p1_2');
    expect(calculateChildPrefix('agy_p1', 3)).toBe('agy_p1_3');
  });

  it('assigns child prefix for agy_p2 correctly', () => {
    expect(calculateChildPrefix('agy_p2', 1)).toBe('agy_p2_1');
    expect(calculateChildPrefix('agy_p2', 2)).toBe('agy_p2_2');
    expect(calculateChildPrefix('agy_p2', 10)).toBe('agy_p2_10');
  });

  it('preserves prefixes with multiple underscores and does not strip suffixes', () => {
    expect(calculateChildPrefix('agy_p1_backup', 1)).toBe('agy_p1_backup_1');
    expect(calculateChildPrefix('antigravity_pool_main_dev', 4)).toBe('antigravity_pool_main_dev_4');
    expect(calculateChildPrefix('profile_with__double__underscores', 2)).toBe('profile_with__double__underscores_2');
  });

  it('falls back safely for null or empty parent prefixes', () => {
    expect(calculateChildPrefix('', 1)).toBe('profile_1');
    expect(calculateChildPrefix(null, 1)).toBe('profile_1');
    expect(calculateChildPrefix(undefined, 2)).toBe('profile_2');
  });

  it('handles non-positive numbers gracefully', () => {
    expect(calculateChildPrefix('agy_p1', 0)).toBe('agy_p1_1');
    expect(calculateChildPrefix('agy_p1', -5)).toBe('agy_p1_1');
  });
});

describe('countOutgoingEdges and getNextChildNumber', () => {
  it('counts exact outgoing edges from source ID', () => {
    const edges = [
      { source: 'node-p1', target: 'node-c1' },
      { source: 'node-p1', target: 'node-c2' },
      { source: 'node-p2', target: 'node-c3' },
    ];

    expect(countOutgoingEdges(edges, 'node-p1')).toBe(2);
    expect(countOutgoingEdges(edges, 'node-p2')).toBe(1);
    expect(countOutgoingEdges(edges, 'node-p3')).toBe(0);
    expect(getNextChildNumber(edges, 'node-p1')).toBe(3);
    expect(getNextChildNumber(edges, 'node-p2')).toBe(2);
    expect(getNextChildNumber(edges, 'node-p3')).toBe(1);
  });

  it('handles null, undefined or empty edges array', () => {
    expect(countOutgoingEdges(null, 'node-1')).toBe(0);
    expect(countOutgoingEdges([], 'node-1')).toBe(0);
    expect(getNextChildNumber([], 'node-1')).toBe(1);
  });
});

describe('validateConnection', () => {
  const nodes = [
    { id: 'p1', data: { prefix: 'agy_p1' } },
    { id: 'p2', data: { prefix: 'agy_p2' } },
    { id: 'c1', data: { prefix: '' } },
    { id: 'c2', data: { prefix: '' } },
  ];

  it('validates valid connection between existing nodes', () => {
    const result = validateConnection(nodes, [], { source: 'p1', target: 'c1' });
    expect(result.valid).toBe(true);
    expect(result.sourceNode.id).toBe('p1');
    expect(result.targetNode.id).toBe('c1');
  });

  it('rejects self-connection', () => {
    const result = validateConnection(nodes, [], { source: 'p1', target: 'p1' });
    expect(result.valid).toBe(false);
    expect(result.reason).toBe('self_connection');
  });

  it('rejects missing source or target endpoints', () => {
    expect(validateConnection(nodes, [], { source: 'p1' }).valid).toBe(false);
    expect(validateConnection(nodes, [], { target: 'c1' }).valid).toBe(false);
    expect(validateConnection(nodes, [], null).valid).toBe(false);
  });

  it('rejects when source node does not exist in graph', () => {
    const result = validateConnection(nodes, [], { source: 'nonexistent', target: 'c1' });
    expect(result.valid).toBe(false);
    expect(result.reason).toBe('source_not_found');
  });

  it('rejects when target node does not exist in graph', () => {
    const result = validateConnection(nodes, [], { source: 'p1', target: 'nonexistent' });
    expect(result.valid).toBe(false);
    expect(result.reason).toBe('target_not_found');
  });

  it('detects already connected edges to prevent duplicate connections', () => {
    const edges = [{ source: 'p1', target: 'c1' }];
    const result = validateConnection(nodes, edges, { source: 'p1', target: 'c1' });
    expect(result.valid).toBe(false);
    expect(result.reason).toBe('already_connected');
  });

  it('rejects multi-parent connections when target already has an incoming edge from another parent', () => {
    const edges = [{ source: 'p1', target: 'c1' }];
    const result = validateConnection(nodes, edges, { source: 'p2', target: 'c1' });
    expect(result.valid).toBe(false);
    expect(result.reason).toBe('target_already_has_parent');
  });
});

describe('applyConnectionPure', () => {
  const initialNodes = [
    { id: 'p1', data: { prefix: 'agy_p1', label: 'agy_p1' } },
    { id: 'p2', data: { prefix: 'agy_p2', label: 'agy_p2' } },
    { id: 'c1', data: { prefix: 'placeholder', label: 'placeholder' } },
    { id: 'c2', data: { prefix: 'placeholder', label: 'placeholder' } },
    { id: 'c3', data: { prefix: 'placeholder', label: 'placeholder' } },
  ];

  it('connects first child to agy_p1 resulting in agy_p1_1', () => {
    const res1 = applyConnectionPure(initialNodes, [], { source: 'p1', target: 'c1' });
    expect(res1.applied).toBe(true);
    expect(res1.childPrefix).toBe('agy_p1_1');
    expect(res1.edges.length).toBe(1);

    const childNode = res1.nodes.find((n) => n.id === 'c1');
    expect(childNode.data.prefix).toBe('agy_p1_1');
    expect(childNode.data.label).toBe('agy_p1_1');
  });

  it('connects repeated children sequentially to agy_p1 resulting in agy_p1_1, agy_p1_2, agy_p1_3', () => {
    const step1 = applyConnectionPure(initialNodes, [], { source: 'p1', target: 'c1' });
    expect(step1.childPrefix).toBe('agy_p1_1');

    const step2 = applyConnectionPure(step1.nodes, step1.edges, { source: 'p1', target: 'c2' });
    expect(step2.childPrefix).toBe('agy_p1_2');
    expect(step2.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p1_2');

    const step3 = applyConnectionPure(step2.nodes, step2.edges, { source: 'p1', target: 'c3' });
    expect(step3.childPrefix).toBe('agy_p1_3');
    expect(step3.nodes.find((n) => n.id === 'c3').data.prefix).toBe('agy_p1_3');
  });

  it('connects child to agy_p2 independently without affecting agy_p1 sequence', () => {
    const step1 = applyConnectionPure(initialNodes, [], { source: 'p1', target: 'c1' });
    const step2 = applyConnectionPure(step1.nodes, step1.edges, { source: 'p2', target: 'c2' });

    expect(step2.childPrefix).toBe('agy_p2_1');
    expect(step2.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(step2.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p2_1');

    const step3 = applyConnectionPure(step2.nodes, step2.edges, { source: 'p2', target: 'c3' });
    expect(step3.childPrefix).toBe('agy_p2_2');
    expect(step3.nodes.find((n) => n.id === 'c3').data.prefix).toBe('agy_p2_2');
  });

  it('rejects multi-parent connection in applyConnectionPure', () => {
    const step1 = applyConnectionPure(initialNodes, [], { source: 'p1', target: 'c1' });
    expect(step1.applied).toBe(true);

    const step2 = applyConnectionPure(step1.nodes, step1.edges, { source: 'p2', target: 'c1' });
    expect(step2.applied).toBe(false);
    expect(step2.reason).toBe('target_already_has_parent');
    expect(step2.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
  });

  it('is completely pure and does not mutate input state arrays', () => {
    const frozenNodes = Object.freeze([...initialNodes.map((n) => Object.freeze({ ...n, data: Object.freeze({ ...n.data }) }))]);
    const frozenEdges = Object.freeze([]);

    const result = applyConnectionPure(frozenNodes, frozenEdges, { source: 'p1', target: 'c1' });
    expect(result.applied).toBe(true);
    expect(result.childPrefix).toBe('agy_p1_1');
    expect(frozenEdges.length).toBe(0);
  });

  it('generates deterministic edge ID via getEdgeId', () => {
    expect(getEdgeId('parent-1', 'child-1')).toBe('edge__parent-1-child-1');
  });

  it('prevents collision on edge removal by allocating next unused child prefix across nodes', () => {
    // Scenario: p1 is connected to c1 (agy_p1_1) and c2 (agy_p1_2)
    const step1 = applyConnectionPure(initialNodes, [], { source: 'p1', target: 'c1' });
    const step2 = applyConnectionPure(step1.nodes, step1.edges, { source: 'p1', target: 'c2' });

    // Edge to c1 is deleted: now only edge to c2 remains (countOutgoingEdges is 1)
    const edgesAfterRemoval = step2.edges.filter((e) => e.target !== 'c1');
    expect(edgesAfterRemoval.length).toBe(1);

    // Connecting c3 must NOT reuse agy_p1_2 (which c2 already holds) even though outgoing count is 1!
    const step3 = applyConnectionPure(step2.nodes, edgesAfterRemoval, { source: 'p1', target: 'c3' });
    expect(step3.applied).toBe(true);
    expect(step3.childPrefix).toBe('agy_p1_3');
    expect(step3.nodes.find((n) => n.id === 'c3').data.prefix).toBe('agy_p1_3');
  });

  describe('VP-7 Descendant Guards & Batch Edge Removal Partitioning', () => {
    it('canDisconnectTarget permits disconnecting leaf child without outgoing edges', () => {
      const edges = [
        { id: 'e1', source: 'p1', target: 'c1' },
      ];
      expect(canDisconnectTarget(edges, 'c1')).toEqual({ allowed: true });
    });

    it('canDisconnectTarget blocks disconnecting node that has outgoing edges (descendants)', () => {
      const edges = [
        { id: 'e1', source: 'p1', target: 'c1' },
        { id: 'e2', source: 'c1', target: 'c2' },
      ];
      const result = canDisconnectTarget(edges, 'c1');
      expect(result.allowed).toBe(false);
      expect(result.reason).toBe('target_has_descendants');
      expect(result.error).toContain('Cannot disconnect profile with attached descendants');
    });

    it('partitionEdgeRemovals safely accepts leaf edge removal', () => {
      const edges = [
        { id: 'e1', source: 'p1', target: 'c1' },
        { id: 'e2', source: 'p1', target: 'c2' },
      ];
      const { safeRemovals, rejectedRemovals } = partitionEdgeRemovals(edges, ['e1']);
      expect(safeRemovals).toHaveLength(1);
      expect(safeRemovals[0].id).toBe('e1');
      expect(rejectedRemovals).toHaveLength(0);
    });

    it('partitionEdgeRemovals rejects non-leaf edge removal when descendants are not in batch', () => {
      const edges = [
        { id: 'e1', source: 'p1', target: 'c1' },
        { id: 'e2', source: 'c1', target: 'c2' },
      ];
      // Only e1 requested: c1 has descendant e2 that remains
      const { safeRemovals, rejectedRemovals } = partitionEdgeRemovals(edges, ['e1']);
      expect(safeRemovals).toHaveLength(0);
      expect(rejectedRemovals).toHaveLength(1);
      expect(rejectedRemovals[0].edge.id).toBe('e1');
      expect(rejectedRemovals[0].reason).toBe('target_has_descendants');
    });

    it('partitionEdgeRemovals permits removing parent edge if child edge is also in removal batch', () => {
      const edges = [
        { id: 'e1', source: 'p1', target: 'c1' },
        { id: 'e2', source: 'c1', target: 'c2' },
      ];
      // Both e1 and e2 requested in same batch
      const { safeRemovals, rejectedRemovals } = partitionEdgeRemovals(edges, ['e1', 'e2']);
      expect(safeRemovals).toHaveLength(2);
      expect(rejectedRemovals).toHaveLength(0);
    });

    it('partitionEdgeRemovals cascades rejection in a 3-tier chain when deep descendant is missing from batch', () => {
      const edges = [
        { id: 'e1', source: 'p1', target: 'c1' },
        { id: 'e2', source: 'c1', target: 'c2' },
      ];
      const edgesChain = [
        { id: 'e1', source: 'p1', target: 'c1' },
        { id: 'e2', source: 'c1', target: 'c2' },
        { id: 'e3', source: 'c2', target: 'c3' },
      ];
      // e1 and e2 requested, but e3 (c2's child) is NOT requested
      const { safeRemovals, rejectedRemovals } = partitionEdgeRemovals(edgesChain, ['e1', 'e2']);
      // e2 is rejected because c2 still has e3. Consequently e1 is rejected because c1 still has e2.
      expect(safeRemovals).toHaveLength(0);
      expect(rejectedRemovals).toHaveLength(2);
    });

    it('partitionEdgeRemovals partitions mixed independent removals: accepts safe and rejects guarded', () => {
      const edges = [
        { id: 'e_safe', source: 'p1', target: 'c_leaf' },
        { id: 'e_guarded', source: 'p2', target: 'c_parent' },
        { id: 'e_child', source: 'c_parent', target: 'c_deep' },
      ];
      // Requesting e_safe and e_guarded (e_child is NOT requested)
      const { safeRemovals, rejectedRemovals } = partitionEdgeRemovals(edges, [
        'e_safe',
        'e_guarded',
      ]);
      expect(safeRemovals.map((e) => e.id)).toEqual(['e_safe']);
      expect(rejectedRemovals.map((r) => r.edge.id)).toEqual(['e_guarded']);
    });
  });
});
