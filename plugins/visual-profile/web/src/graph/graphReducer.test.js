import { describe, it, expect } from 'vitest';
import {
  graphReducer,
  connectNodesAtomic,
  createInitialGraphState,
} from './graphReducer';
import {
  getNextAvailableRootPrefix,
  getNextAvailableChildPrefix,
} from './connection';

describe('graphReducer and connectNodesAtomic', () => {
  const baseNodes = [
    { id: 'p1', data: { prefix: 'agy_p1', label: 'agy_p1', isRoot: true } },
    { id: 'p2', data: { prefix: 'agy_p2', label: 'agy_p2', isRoot: true } },
    { id: 'p_under', data: { prefix: 'custom_pool_backup_01', label: 'custom_pool_backup_01', isRoot: true } },
    { id: 'c1', data: { prefix: 'unlinked_1', label: 'unlinked_1', isRoot: false } },
    { id: 'c2', data: { prefix: 'unlinked_2', label: 'unlinked_2', isRoot: false } },
    { id: 'c3', data: { prefix: 'unlinked_3', label: 'unlinked_3', isRoot: false } },
  ];

  it('guarantees reducer purity: replaying identical action returns identical state and node IDs', () => {
    const initialState = createInitialGraphState(baseNodes, []);
    const action = {
      type: 'ADD_ROOT_NODE',
      id: 'p_fixed_test_123',
      customPrefix: 'replay_test',
    };

    // Simulate React StrictMode double invocation
    const run1 = graphReducer(initialState, action);
    const run2 = graphReducer(initialState, action);

    expect(run1).toEqual(run2);
    expect(run1.nodes[run1.nodes.length - 1].id).toBe('p_fixed_test_123');
    expect(run2.nodes[run2.nodes.length - 1].id).toBe('p_fixed_test_123');
  });

  it('takes parent prefix from current nds and outgoing count from current eds', () => {
    const initialState = createInitialGraphState(baseNodes, []);

    const state1 = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'c1' },
    });

    expect(state1.edges.length).toBe(1);
    expect(state1.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');

    const state2 = graphReducer(state1, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'c2' },
    });

    expect(state2.edges.length).toBe(2);
    expect(state2.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p1_2');

    const state3 = graphReducer(state2, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'c3' },
    });

    expect(state3.edges.length).toBe(3);
    expect(state3.nodes.find((n) => n.id === 'c3').data.prefix).toBe('agy_p1_3');
  });

  it('connects children to agy_p2 independently', () => {
    const initialState = createInitialGraphState(baseNodes, []);

    const state1 = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p2', target: 'c1' },
    });
    expect(state1.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p2_1');

    const state2 = graphReducer(state1, {
      type: 'CONNECT',
      connection: { source: 'p2', target: 'c2' },
    });
    expect(state2.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p2_2');
  });

  it('preserves full parent prefixes with underscores', () => {
    const initialState = createInitialGraphState(baseNodes, []);

    const state1 = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p_under', target: 'c1' },
    });
    expect(state1.nodes.find((n) => n.id === 'c1').data.prefix).toBe('custom_pool_backup_01_1');

    const state2 = graphReducer(state1, {
      type: 'CONNECT',
      connection: { source: 'p_under', target: 'c2' },
    });
    expect(state2.nodes.find((n) => n.id === 'c2').data.prefix).toBe('custom_pool_backup_01_2');
  });

  it('does NOT rename child when duplicate edge is connected', () => {
    const initialState = createInitialGraphState(baseNodes, []);

    const state1 = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'c1' },
    });
    expect(state1.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(state1.edges.length).toBe(1);

    // Attempting duplicate connection
    const state2 = graphReducer(state1, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'c1' },
    });

    // Child must NOT be renamed, edge count unchanged, state unchanged
    expect(state2.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(state2.edges.length).toBe(1);
    expect(state2).toBe(state1);
  });

  it('rejects multi-parent child: target already has incoming edge from another parent', () => {
    const initialState = createInitialGraphState(baseNodes, []);

    // Connect p1 -> c1 -> agy_p1_1
    const state1 = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'c1' },
    });
    expect(state1.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');

    // Attempt connecting p2 -> c1 (c1 already has parent p1)
    const state2 = graphReducer(state1, {
      type: 'CONNECT',
      connection: { source: 'p2', target: 'c1' },
    });

    // Multi-parent rejected: c1 retains agy_p1_1, edge count remains 1, state unchanged
    expect(state2.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(state2.edges.length).toBe(1);
    expect(state2).toBe(state1);
  });

  it('does NOT rename child when invalid edge is connected (missing source, missing target, self)', () => {
    const initialState = createInitialGraphState(baseNodes, []);

    // Self connection
    const stateSelf = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'p1' },
    });
    expect(stateSelf).toBe(initialState);
    expect(stateSelf.nodes.find((n) => n.id === 'p1').data.prefix).toBe('agy_p1');

    // Missing source
    const stateNoSource = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'nonexistent', target: 'c1' },
    });
    expect(stateNoSource).toBe(initialState);
    expect(stateNoSource.nodes.find((n) => n.id === 'c1').data.prefix).toBe('unlinked_1');

    // Missing target
    const stateNoTarget = graphReducer(initialState, {
      type: 'CONNECT',
      connection: { source: 'p1', target: 'nonexistent' },
    });
    expect(stateNoTarget).toBe(initialState);
  });

  it('chooses next available root prefix avoiding existing collisions', () => {
    // Existing nodes already use agy_p1, agy_p2, and agy_p3 (custom)
    const nodesWithP3 = [
      ...baseNodes,
      { id: 'p3_custom', data: { prefix: 'agy_p3', label: 'agy_p3', isRoot: true } },
    ];

    expect(getNextAvailableRootPrefix(nodesWithP3)).toBe('agy_p4');

    const state = createInitialGraphState(nodesWithP3, []);
    const updated = graphReducer(state, {
      type: 'ADD_ROOT_NODE',
      id: 'p4',
    });

    const added = updated.nodes.find((n) => n.id === 'p4');
    expect(added.data.prefix).toBe('agy_p4');
  });

  it('fills gaps in root prefixes cleanly when prefix holes exist', () => {
    // Nodes has agy_p1 and agy_p3 (agy_p2 is available)
    const nodesWithGap = [
      { id: 'p1', data: { prefix: 'agy_p1', isRoot: true } },
      { id: 'p3', data: { prefix: 'agy_p3', isRoot: true } },
    ];
    expect(getNextAvailableRootPrefix(nodesWithGap)).toBe('agy_p2');
  });

  it('chooses next available child prefix avoiding collisions and filling gaps', () => {
    const nodes = [
      { id: 'c1', data: { prefix: 'unlinked_1', isRoot: false } },
      { id: 'c3', data: { prefix: 'unlinked_3', isRoot: false } },
    ];
    expect(getNextAvailableChildPrefix(nodes)).toBe('unlinked_2');

    const state = createInitialGraphState(nodes, []);
    const updated = graphReducer(state, {
      type: 'ADD_CHILD_NODE',
      id: 'c2',
    });
    expect(updated.nodes.find((n) => n.id === 'c2').data.prefix).toBe('unlinked_2');
  });

  it('supports rapid creation with unique external IDs without collision', () => {
    let state = createInitialGraphState([], []);
    for (let i = 1; i <= 20; i++) {
      state = graphReducer(state, {
        type: 'ADD_ROOT_NODE',
        id: `root_${i}`,
      });
    }
    expect(state.nodes.length).toBe(20);
    const prefixes = state.nodes.map((n) => n.data.prefix);
    const uniquePrefixes = new Set(prefixes);
    expect(uniquePrefixes.size).toBe(20);
    expect(prefixes[0]).toBe('agy_p1');
    expect(prefixes[19]).toBe('agy_p20');
  });
});
