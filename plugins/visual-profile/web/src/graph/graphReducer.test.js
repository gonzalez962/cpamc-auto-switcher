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

  it('updates node prefix and dirty state atomically via UPDATE_NODE_PREFIX', () => {
    const realNodes = [
      {
        id: 'acc1.json',
        data: {
          prefix: 'agy_p1',
          initialPrefix: 'agy_p1',
          label: 'agy_p1',
          fileName: 'acc1.json',
          isDirty: false,
          isSynthetic: false,
        },
      },
    ];
    const state = createInitialGraphState(realNodes, []);

    // 1. Edit prefix to a new valid string
    const updated1 = graphReducer(state, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'acc1.json',
      prefix: 'custom_pool_99',
    });
    const node1 = updated1.nodes.find((n) => n.id === 'acc1.json');
    expect(node1.data.prefix).toBe('custom_pool_99');
    expect(node1.data.label).toBe('custom_pool_99');
    expect(node1.data.isDirty).toBe(true);

    // 2. Edit prefix back to initialPrefix -> isDirty resets to false
    const updated2 = graphReducer(updated1, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'acc1.json',
      prefix: 'agy_p1',
    });
    const node2 = updated2.nodes.find((n) => n.id === 'acc1.json');
    expect(node2.data.prefix).toBe('agy_p1');
    expect(node2.data.label).toBe('agy_p1');
    expect(node2.data.isDirty).toBe(false);

    // 3. Exact allowed empty per API
    const updatedEmpty = graphReducer(updated2, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'acc1.json',
      prefix: '',
    });
    const nodeEmpty = updatedEmpty.nodes.find((n) => n.id === 'acc1.json');
    expect(nodeEmpty.data.prefix).toBe('');
    expect(nodeEmpty.data.label).toBe('acc1.json'); // falls back to fileName
    expect(nodeEmpty.data.isDirty).toBe(true);

    // 4. Reject invalid prefix with spaces
    const rejected = graphReducer(updated2, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'acc1.json',
      prefix: 'invalid prefix with spaces',
    });
    expect(rejected).toBe(updated2);
  });

  it('updates child dirty state atomically when real nodes are connected', () => {
    const realNodes = [
      {
        id: 'parent.json',
        data: {
          prefix: 'agy_p1',
          initialPrefix: 'agy_p1',
          label: 'agy_p1',
          isRoot: true,
          isDirty: false,
        },
      },
      {
        id: 'child.json',
        data: {
          prefix: '',
          initialPrefix: '',
          label: 'child.json',
          isRoot: false,
          isDirty: false,
        },
      },
    ];
    const state = createInitialGraphState(realNodes, []);

    const connected = graphReducer(state, {
      type: 'CONNECT',
      connection: { source: 'parent.json', target: 'child.json' },
    });

    const childNode = connected.nodes.find((n) => n.id === 'child.json');
    expect(childNode.data.prefix).toBe('agy_p1_1');
    expect(childNode.data.label).toBe('agy_p1_1');
    expect(childNode.data.isDirty).toBe(true);
  });

  it('rejects parent rename while attached children exist to preserve prefix coherence', () => {
    const nodes = [
      {
        id: 'parent.json',
        data: { prefix: 'agy_p1', initialPrefix: 'agy_p1', label: 'agy_p1', isRoot: true },
      },
      {
        id: 'child.json',
        data: { prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', label: 'agy_p1_1', isRoot: false },
      },
    ];
    const edges = [{ id: 'edge__parent.json-child.json', source: 'parent.json', target: 'child.json' }];
    const state = createInitialGraphState(nodes, edges);

    // Attempting to rename parent while child is attached must be rejected
    const nextState = graphReducer(state, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'parent.json',
      prefix: 'renamed_parent',
    });

    expect(nextState).toBe(state);
    expect(nextState.nodes.find((n) => n.id === 'parent.json').data.prefix).toBe('agy_p1');
  });

  it('detaches incoming edge when child node prefix is manually edited', () => {
    const nodes = [
      {
        id: 'parent.json',
        data: { prefix: 'agy_p1', initialPrefix: 'agy_p1', label: 'agy_p1', isRoot: true },
      },
      {
        id: 'child.json',
        data: { prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', label: 'agy_p1_1', isRoot: false },
      },
    ];
    const edges = [{ id: 'edge__parent.json-child.json', source: 'parent.json', target: 'child.json' }];
    const state = createInitialGraphState(nodes, edges);

    // Manually edit child to an unlinked custom prefix
    const nextState = graphReducer(state, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'child.json',
      prefix: 'custom_independent_pool',
    });

    // Incoming edge must be detached to prevent a false parent edge
    expect(nextState.edges.length).toBe(0);
    const child = nextState.nodes.find((n) => n.id === 'child.json');
    expect(child.data.prefix).toBe('custom_independent_pool');
    expect(child.data.isDirty).toBe(true);
    expect(child.data.isRoot).toBe(true);
  });

  it('permits UPDATE_NODE_PREFIX to an existing shared prefix for pool rotation', () => {
    const nodes = [
      {
        id: 'acc1.json',
        data: { prefix: 'agy_p1', label: 'agy_p1' },
      },
      {
        id: 'acc2.json',
        data: { prefix: 'agy_p2', label: 'agy_p2' },
      },
    ];
    const state = createInitialGraphState(nodes, []);

    // Editing acc2 to join the agy_p1 pool is valid in CLIProxyAPI
    const nextState = graphReducer(state, {
      type: 'UPDATE_NODE_PREFIX',
      id: 'acc2.json',
      prefix: 'agy_p1',
    });

    const updatedNode = nextState.nodes.find((n) => n.id === 'acc2.json');
    expect(updatedNode.data.prefix).toBe('agy_p1');
    expect(updatedNode.data.label).toBe('agy_p1');
    expect(updatedNode.data.isDirty).toBe(true);
  });

  it('rejects connection when source node has empty prefix and does not fall back to filename', () => {
    const nodes = [
      {
        id: 'source-no-prefix.json',
        data: { prefix: '', label: 'source-no-prefix.json', fileName: 'source-no-prefix.json' },
      },
      {
        id: 'child.json',
        data: { prefix: '', label: 'child.json', fileName: 'child.json' },
      },
    ];
    const state = createInitialGraphState(nodes, []);

    const nextState = graphReducer(state, {
      type: 'CONNECT',
      connection: { source: 'source-no-prefix.json', target: 'child.json' },
    });

    // Connection must be rejected, child prefix must remain unchanged (no fallback to filename_1)
    expect(nextState).toBe(state);
    expect(nextState.edges.length).toBe(0);
    expect(nextState.nodes.find((n) => n.id === 'child.json').data.prefix).toBe('');
  });

  it('connects without collision after an edge was deleted', () => {
    const nodes = [
      { id: 'p1', data: { prefix: 'agy_p1' } },
      { id: 'c1', data: { prefix: '' } },
      { id: 'c2', data: { prefix: '' } },
      { id: 'c3', data: { prefix: '' } },
    ];
    let state = createInitialGraphState(nodes, []);

    // Connect c1 and c2
    state = graphReducer(state, { type: 'CONNECT', connection: { source: 'p1', target: 'c1' } });
    state = graphReducer(state, { type: 'CONNECT', connection: { source: 'p1', target: 'c2' } });
    expect(state.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(state.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p1_2');

    // Remove edge to c1 (simulating edge deletion in React Flow)
    state = {
      ...state,
      edges: state.edges.filter((e) => e.target !== 'c1'),
    };
    expect(state.edges.length).toBe(1);

    // Connect c3: must allocate agy_p1_3, NOT collide with agy_p1_2!
    state = graphReducer(state, { type: 'CONNECT', connection: { source: 'p1', target: 'c3' } });
    expect(state.nodes.find((n) => n.id === 'c3').data.prefix).toBe('agy_p1_3');
  });

  it('marks autogenerated nodes as synthetic', () => {
    let state = createInitialGraphState([], []);
    state = graphReducer(state, { type: 'ADD_ROOT_NODE', id: 'synth_root' });
    state = graphReducer(state, { type: 'ADD_CHILD_NODE', id: 'synth_child' });

    expect(state.nodes[0].data.isSynthetic).toBe(true);
    expect(state.nodes[1].data.isSynthetic).toBe(true);
  });

  it('loads graph state via LOAD_GRAPH action', () => {
    const state = createInitialGraphState([], []);
    const loaded = graphReducer(state, {
      type: 'LOAD_GRAPH',
      nodes: [{ id: 'f1.json', data: { prefix: 'p1' } }],
      edges: [{ id: 'e1', source: 'a', target: 'b' }],
    });
    expect(loaded.nodes.length).toBe(1);
    expect(loaded.edges.length).toBe(1);
  });

  describe('RECONCILE_SAVED_NODES - State Reconciliation & Draft Preservation', () => {
    it('advances initialPrefix and marks clean when save succeeds and node was not edited while pending', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'acc1.json',
            data: {
              fileName: 'acc1.json',
              prefix: 'new_prefix_1',
              initialPrefix: 'old_prefix_1',
              isDirty: true,
            },
          },
        ],
        []
      );

      const nextState = graphReducer(state, {
        type: 'RECONCILE_SAVED_NODES',
        successful: [{ name: 'acc1.json', prefix: 'new_prefix_1' }],
        failed: [],
      });

      const node = nextState.nodes.find((n) => n.id === 'acc1.json');
      expect(node.data.initialPrefix).toBe('new_prefix_1');
      expect(node.data.prefix).toBe('new_prefix_1');
      expect(node.data.isDirty).toBe(false);
    });

    it('CRITICAL: preserves latest draft prefix and retains isDirty: true if node was edited while save was pending', () => {
      // Step 1: User modified prefix to 'submitted_val' and clicked save (request is in-flight)
      // Step 2: While request is in flight, user further edited prefix to 'concurrent_draft_val'
      const state = createInitialGraphState(
        [
          {
            id: 'acc1.json',
            data: {
              fileName: 'acc1.json',
              prefix: 'concurrent_draft_val', // Latest draft made while pending!
              initialPrefix: 'old_prefix_1',
              isDirty: true,
            },
          },
        ],
        []
      );

      // Step 3: Save response arrives with success for 'submitted_val'
      const nextState = graphReducer(state, {
        type: 'RECONCILE_SAVED_NODES',
        successful: [{ name: 'acc1.json', prefix: 'submitted_val' }],
        failed: [],
      });

      const node = nextState.nodes.find((n) => n.id === 'acc1.json');
      // Server initialPrefix advanced to submitted_val
      expect(node.data.initialPrefix).toBe('submitted_val');
      // Latest draft prefix is PRESERVED, not clobbered!
      expect(node.data.prefix).toBe('concurrent_draft_val');
      // Node remains dirty because current draft differs from saved initialPrefix!
      expect(node.data.isDirty).toBe(true);
    });

    it('CRITICAL: retains original initialPrefix and dirty status when save fails', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'acc2.json',
            data: {
              fileName: 'acc2.json',
              prefix: 'failed_prefix',
              initialPrefix: 'orig_prefix',
              isDirty: true,
            },
          },
        ],
        []
      );

      const nextState = graphReducer(state, {
        type: 'RECONCILE_SAVED_NODES',
        successful: [],
        failed: [{ name: 'acc2.json', error: '409 Conflict' }],
      });

      const node = nextState.nodes.find((n) => n.id === 'acc2.json');
      // Original initialPrefix MUST be retained
      expect(node.data.initialPrefix).toBe('orig_prefix');
      expect(node.data.prefix).toBe('failed_prefix');
      expect(node.data.isDirty).toBe(true);
    });

    it('handles multi-file partial failure: successful file advances and failed file retains initialPrefix', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'acc1.json',
            data: {
              fileName: 'acc1.json',
              prefix: 'p1_saved',
              initialPrefix: 'p1_init',
              isDirty: true,
            },
          },
          {
            id: 'acc2.json',
            data: {
              fileName: 'acc2.json',
              prefix: 'p2_failed',
              initialPrefix: 'p2_init',
              isDirty: true,
            },
          },
          {
            id: 'unrelated.json',
            data: {
              fileName: 'unrelated.json',
              prefix: 'unrelated_prefix',
              initialPrefix: 'unrelated_prefix',
              isDirty: false,
            },
          },
        ],
        [{ id: 'e1', source: 'acc1.json', target: 'acc2.json' }]
      );

      const nextState = graphReducer(state, {
        type: 'RECONCILE_SAVED_NODES',
        successful: [{ name: 'acc1.json', prefix: 'p1_saved' }],
        failed: [{ name: 'acc2.json', error: '404 Not Found' }],
      });

      const n1 = nextState.nodes.find((n) => n.id === 'acc1.json');
      const n2 = nextState.nodes.find((n) => n.id === 'acc2.json');
      const n3 = nextState.nodes.find((n) => n.id === 'unrelated.json');

      expect(n1.data.initialPrefix).toBe('p1_saved');
      expect(n1.data.isDirty).toBe(false);

      expect(n2.data.initialPrefix).toBe('p2_init');
      expect(n2.data.isDirty).toBe(true);

      expect(n3.data.initialPrefix).toBe('unrelated_prefix');
      expect(n3.data.isDirty).toBe(false);

      // Edges must be completely preserved (no blind reset)
      expect(nextState.edges.length).toBe(1);
    });

    it('reconciles empty string prefix correctly', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'cleared.json',
            data: {
              fileName: 'cleared.json',
              prefix: '',
              initialPrefix: 'was_something',
              isDirty: true,
            },
          },
        ],
        []
      );

      const nextState = graphReducer(state, {
        type: 'RECONCILE_SAVED_NODES',
        successful: [{ name: 'cleared.json', prefix: '' }],
        failed: [],
      });

      const node = nextState.nodes.find((n) => n.id === 'cleared.json');
      expect(node.data.initialPrefix).toBe('');
      expect(node.data.prefix).toBe('');
      expect(node.data.isDirty).toBe(false);
    });
  });

  describe('VP-7 Edge Removal, Disconnect & Descendant Guard Semantics', () => {
    it('DISCONNECT_NODE atomically removes incoming edge and sets prefix to empty, label to fileName, and marks isDirty', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'p1.json',
            data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
          },
          {
            id: 'c1.json',
            data: {
              fileName: 'c1.json',
              prefix: 'agy_p1_1',
              initialPrefix: 'agy_p1_1',
              label: 'agy_p1_1',
              isRoot: false,
              isDirty: false,
            },
          },
        ],
        [{ id: 'e1', source: 'p1.json', target: 'c1.json' }]
      );

      const next = graphReducer(state, { type: 'DISCONNECT_NODE', nodeId: 'c1.json' });

      // Edge is atomically removed
      expect(next.edges).toHaveLength(0);

      // Child target node is reset: empty prefix, label to fileName, isDirty true, isRoot false
      const child = next.nodes.find((n) => n.id === 'c1.json');
      expect(child.data.prefix).toBe('');
      expect(child.data.label).toBe('c1.json');
      expect(child.data.isDirty).toBe(true); // '' !== 'agy_p1_1'
      expect(child.data.isRoot).toBe(false);
    });

    it('DISCONNECT_NODE resets isDirty to false when initialPrefix was empty', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'p1.json',
            data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
          },
          {
            id: 'c1.json',
            data: {
              fileName: 'c1.json',
              prefix: 'agy_p1_1',
              initialPrefix: '', // Loaded originally with empty prefix
              label: 'agy_p1_1',
              isRoot: false,
              isDirty: true,
            },
          },
        ],
        [{ id: 'e1', source: 'p1.json', target: 'c1.json' }]
      );

      const next = graphReducer(state, { type: 'DISCONNECT_NODE', nodeId: 'c1.json' });

      expect(next.edges).toHaveLength(0);
      const child = next.nodes.find((n) => n.id === 'c1.json');
      expect(child.data.prefix).toBe('');
      expect(child.data.isDirty).toBe(false); // '' === initialPrefix ''
    });

    it('DISCONNECT_NODE preserves original state when target has attached descendants', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'p1.json',
            data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
          },
          {
            id: 'c1.json',
            data: {
              fileName: 'c1.json',
              prefix: 'agy_p1_1',
              initialPrefix: 'agy_p1_1',
              label: 'agy_p1_1',
              isRoot: false,
            },
          },
          {
            id: 'c2.json',
            data: {
              fileName: 'c2.json',
              prefix: 'agy_p1_1_1',
              initialPrefix: 'agy_p1_1_1',
              label: 'agy_p1_1_1',
              isRoot: false,
            },
          },
        ],
        [
          { id: 'e1', source: 'p1.json', target: 'c1.json' },
          { id: 'e2', source: 'c1.json', target: 'c2.json' }, // c1 has descendant c2!
        ]
      );

      // Attempting to disconnect c1 while c1 still has descendant c2 MUST be rejected
      const next = graphReducer(state, { type: 'DISCONNECT_NODE', nodeId: 'c1.json' });

      // Edges and nodes remain 100% untouched
      expect(next.edges).toHaveLength(2);
      const c1 = next.nodes.find((n) => n.id === 'c1.json');
      expect(c1.data.prefix).toBe('agy_p1_1');
      expect(c1.data.label).toBe('agy_p1_1');
    });

    it('REMOVE_EDGES removes edge and clears target node prefix atomically', () => {
      const state = createInitialGraphState(
        [
          {
            id: 'p1.json',
            data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
          },
          {
            id: 'c1.json',
            data: {
              fileName: 'c1.json',
              prefix: 'agy_p1_1',
              initialPrefix: 'agy_p1_1',
              label: 'agy_p1_1',
              isRoot: false,
            },
          },
        ],
        [{ id: 'edge__p1-c1', source: 'p1.json', target: 'c1.json' }]
      );

      const next = graphReducer(state, {
        type: 'REMOVE_EDGES',
        edgeIds: ['edge__p1-c1'],
      });

      expect(next.edges).toHaveLength(0);
      const c1 = next.nodes.find((n) => n.id === 'c1.json');
      expect(c1.data.prefix).toBe('');
      expect(c1.data.label).toBe('c1.json');
      expect(c1.data.isDirty).toBe(true);
    });

    it('REMOVE_EDGES protects against descendant guard and preserves original state for rejected removal', () => {
      const state = createInitialGraphState(
        [
          { id: 'p1', data: { prefix: 'agy_p1' } },
          { id: 'c1', data: { prefix: 'agy_p1_1' } },
          { id: 'c2', data: { prefix: 'agy_p1_1_1' } },
        ],
        [
          { id: 'e1', source: 'p1', target: 'c1' },
          { id: 'e2', source: 'c1', target: 'c2' },
        ]
      );

      // Only e1 requested for removal: target c1 has surviving descendant e2
      const next = graphReducer(state, {
        type: 'REMOVE_EDGES',
        edgeIds: ['e1'],
      });

      // Guard blocks removal, state preserved
      expect(next.edges).toHaveLength(2);
      expect(next.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    });

    it('REMOVE_EDGES safely removes both parent and child edges in a batch removal', () => {
      const state = createInitialGraphState(
        [
          { id: 'p1', data: { prefix: 'agy_p1' } },
          { id: 'c1', data: { prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1' } },
          { id: 'c2', data: { prefix: 'agy_p1_1_1', initialPrefix: 'agy_p1_1_1' } },
        ],
        [
          { id: 'e1', source: 'p1', target: 'c1' },
          { id: 'e2', source: 'c1', target: 'c2' },
        ]
      );

      // Batch removal of e1 and e2 together: both can be removed
      const next = graphReducer(state, {
        type: 'REMOVE_EDGES',
        edgeIds: ['e1', 'e2'],
      });

      expect(next.edges).toHaveLength(0);
      expect(next.nodes.find((n) => n.id === 'c1').data.prefix).toBe('');
      expect(next.nodes.find((n) => n.id === 'c2').data.prefix).toBe('');
    });

    it('handles reconnecting to same parent after disconnect, resetting isDirty when matching initialPrefix', () => {
      // 1. Initial graph with parent p1 and linked child c1 (initialPrefix 'agy_p1_1')
      const initial = createInitialGraphState(
        [
          {
            id: 'p1.json',
            data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
          },
          {
            id: 'c1.json',
            data: {
              fileName: 'c1.json',
              prefix: 'agy_p1_1',
              initialPrefix: 'agy_p1_1',
              label: 'agy_p1_1',
              isRoot: false,
              isDirty: false,
            },
          },
        ],
        [{ id: 'e1', source: 'p1.json', target: 'c1.json' }]
      );

      // 2. Disconnect c1
      const disconnected = graphReducer(initial, {
        type: 'DISCONNECT_NODE',
        nodeId: 'c1.json',
      });
      const discChild = disconnected.nodes.find((n) => n.id === 'c1.json');
      expect(discChild.data.prefix).toBe('');
      expect(discChild.data.isDirty).toBe(true);

      // 3. Reconnect c1 to p1 again
      const reconnected = graphReducer(disconnected, {
        type: 'CONNECT',
        connection: { source: 'p1.json', target: 'c1.json' },
      });
      const recChild = reconnected.nodes.find((n) => n.id === 'c1.json');
      expect(recChild.data.prefix).toBe('agy_p1_1');
      // DIRTY RESET: matches initialPrefix 'agy_p1_1', so isDirty must reset to false!
      expect(recChild.data.isDirty).toBe(false);
      expect(reconnected.edges).toHaveLength(1);
    });

    it('handles reconnecting to other parent after disconnect, keeping isDirty true', () => {
      const initial = createInitialGraphState(
        [
          {
            id: 'p1.json',
            data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
          },
          {
            id: 'p2.json',
            data: { fileName: 'p2.json', prefix: 'agy_p2', initialPrefix: 'agy_p2', isRoot: true },
          },
          {
            id: 'c1.json',
            data: {
              fileName: 'c1.json',
              prefix: 'agy_p1_1',
              initialPrefix: 'agy_p1_1',
              label: 'agy_p1_1',
              isRoot: false,
              isDirty: false,
            },
          },
        ],
        [{ id: 'e1', source: 'p1.json', target: 'c1.json' }]
      );

      // Disconnect from p1
      const disconnected = graphReducer(initial, {
        type: 'DISCONNECT_NODE',
        nodeId: 'c1.json',
      });

      // Reconnect to p2
      const reconnected = graphReducer(disconnected, {
        type: 'CONNECT',
        connection: { source: 'p2.json', target: 'c1.json' },
      });

      const child = reconnected.nodes.find((n) => n.id === 'c1.json');
      expect(child.data.prefix).toBe('agy_p2_1');
      // Differs from initialPrefix 'agy_p1_1', so isDirty must be true!
      expect(child.data.isDirty).toBe(true);
    });

    it('handles synthetic demo nodes disconnect and reconnect', () => {
      const demoState = createInitialGraphState(
        [
          { id: 'p1', data: { prefix: 'agy_p1', isRoot: true, isSynthetic: true } },
          {
            id: 'c1',
            data: {
              prefix: 'unlinked_1',
              initialPrefix: 'unlinked_1',
              label: 'unlinked_1',
              isRoot: false,
              isSynthetic: true,
            },
          },
        ],
        []
      );

      // Connect c1 to p1
      const connected = graphReducer(demoState, {
        type: 'CONNECT',
        connection: { source: 'p1', target: 'c1' },
      });
      expect(connected.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');

      // Disconnect c1
      const disconnected = graphReducer(connected, {
        type: 'DISCONNECT_NODE',
        nodeId: 'c1',
      });
      expect(disconnected.edges).toHaveLength(0);
      const c1 = disconnected.nodes.find((n) => n.id === 'c1');
      expect(c1.data.prefix).toBe('');
      expect(c1.data.label).toBe('c1');
    });

    it('sets graphFeedback on rejected removals and masks email filenames in feedback', () => {
      const state = createInitialGraphState(
        [
          { id: 'p1', data: { prefix: 'agy_p1' } },
          { id: 'developer.user@openai.com.json', data: { prefix: 'agy_p1_1' } },
          { id: 'c_deep', data: { prefix: 'agy_p1_1_1' } },
        ],
        [
          { id: 'e1', source: 'p1', target: 'developer.user@openai.com.json' },
          { id: 'e2', source: 'developer.user@openai.com.json', target: 'c_deep' },
        ]
      );

      const next = graphReducer(state, {
        type: 'DISCONNECT_NODE',
        nodeId: 'developer.user@openai.com.json',
      });

      expect(next.edges).toHaveLength(2);
      expect(next.graphFeedback).toContain('dev...ser@openai.com.json');
      expect(next.graphFeedback).not.toContain('developer.user@openai.com.json');
      expect(next.graphFeedback).toContain('with attached descendants');
    });

    it('clears stale graphFeedback upon subsequent successful removal or action', () => {
      const stateWithFeedback = {
        ...createInitialGraphState(
          [
            { id: 'p1', data: { prefix: 'agy_p1' } },
            { id: 'c1', data: { prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1' } },
          ],
          [{ id: 'e1', source: 'p1', target: 'c1' }]
        ),
        graphFeedback: 'Stale error message from previous rejected action',
      };

      // Safe removal clears feedback
      const next = graphReducer(stateWithFeedback, {
        type: 'REMOVE_EDGES',
        edgeIds: ['e1'],
      });

      expect(next.edges).toHaveLength(0);
      expect(next.graphFeedback).toBeNull();
    });

    it('handles rapid sequential disconnections on pure state snapshot without false rejection', () => {
      // 3-tier hierarchy: p1 -> c1 -> c2
      const state = createInitialGraphState(
        [
          { id: 'p1', data: { prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true } },
          { id: 'c1', data: { prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', isRoot: false } },
          { id: 'c2', data: { prefix: 'agy_p1_1_1', initialPrefix: 'agy_p1_1_1', isRoot: false } },
        ],
        [
          { id: 'e1', source: 'p1', target: 'c1' },
          { id: 'e2', source: 'c1', target: 'c2' },
        ]
      );

      // Step 1: Disconnect leaf c2
      const step1 = graphReducer(state, {
        type: 'DISCONNECT_NODE',
        nodeId: 'c2',
      });
      expect(step1.edges).toHaveLength(1);
      expect(step1.nodes.find((n) => n.id === 'c2').data.prefix).toBe('');

      // Step 2: Immediately disconnect c1 without intermediate re-render
      // Since step1.edges no longer contains e2, c1 has zero descendants!
      const step2 = graphReducer(step1, {
        type: 'DISCONNECT_NODE',
        nodeId: 'c1',
      });
      expect(step2.edges).toHaveLength(0);
      expect(step2.nodes.find((n) => n.id === 'c1').data.prefix).toBe('');
      expect(step2.graphFeedback).toBeNull();
    });
  });
});
