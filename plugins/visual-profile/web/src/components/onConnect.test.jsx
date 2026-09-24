import React, { useReducer, useCallback } from 'react';
import { renderHook, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { graphReducer, createInitialGraphState } from '../graph/graphReducer';
import { countOutgoingEdges } from '../graph/connection';

/**
 * Hook reproducing the exact atomic graph reducer design from ProfileGraph.
 * Provides atomic onConnect, functional setNodes, and functional setEdges.
 */
function useProfileConnectionHarness(initialNodes, initialEdges = []) {
  const [graphState, dispatch] = useReducer(
    graphReducer,
    createInitialGraphState(initialNodes, initialEdges)
  );

  const { nodes, edges } = graphState;

  const setNodes = useCallback((updater) => {
    dispatch({ type: 'SET_NODES', updater });
  }, []);

  const setEdges = useCallback((updater) => {
    dispatch({ type: 'SET_EDGES', updater });
  }, []);

  const onConnect = useCallback((connection) => {
    dispatch({ type: 'CONNECT', connection });
  }, []);

  return { nodes, edges, onConnect, setNodes, setEdges };
}

describe('Atomic Graph Architecture - onConnect, Prefix Assignment, and Guard Tests', () => {
  const baseNodes = [
    { id: 'p1', data: { prefix: 'agy_p1', label: 'agy_p1', isRoot: true } },
    { id: 'p2', data: { prefix: 'agy_p2', label: 'agy_p2', isRoot: true } },
    { id: 'p_complex', data: { prefix: 'antigravity_pool_main_us_east', label: 'antigravity_pool_main_us_east', isRoot: true } },
    { id: 'c1', data: { prefix: 'unlinked_1', label: 'unlinked_1', isRoot: false } },
    { id: 'c2', data: { prefix: 'unlinked_2', label: 'unlinked_2', isRoot: false } },
    { id: 'c3', data: { prefix: 'unlinked_3', label: 'unlinked_3', isRoot: false } },
    { id: 'c4', data: { prefix: 'unlinked_4', label: 'unlinked_4', isRoot: false } },
  ];

  it('tests agy_p1 connecting single and repeated children: agy_p1_1, agy_p1_2, agy_p1_3', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    // Connect child 1 to agy_p1
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c1' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(countOutgoingEdges(result.current.edges, 'p1')).toBe(1);

    // Connect child 2 to agy_p1
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c2' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p1_2');
    expect(countOutgoingEdges(result.current.edges, 'p1')).toBe(2);

    // Connect child 3 to agy_p1
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c3' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c3').data.prefix).toBe('agy_p1_3');
    expect(countOutgoingEdges(result.current.edges, 'p1')).toBe(3);
  });

  it('tests agy_p2 connecting children: agy_p2_1, agy_p2_2 independently', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    act(() => {
      result.current.onConnect({ source: 'p2', target: 'c1' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p2_1');

    act(() => {
      result.current.onConnect({ source: 'p2', target: 'c2' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p2_2');
  });

  it('preserves exact parent prefix with underscores: antigravity_pool_main_us_east', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    act(() => {
      result.current.onConnect({ source: 'p_complex', target: 'c1' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe(
      'antigravity_pool_main_us_east_1'
    );

    act(() => {
      result.current.onConnect({ source: 'p_complex', target: 'c2' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c2').data.prefix).toBe(
      'antigravity_pool_main_us_east_2'
    );
  });

  it('handles rapid repeated connections in the same act/batch without race or stale counts', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c1' });
      result.current.onConnect({ source: 'p1', target: 'c2' });
    });

    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(result.current.nodes.find((n) => n.id === 'c2').data.prefix).toBe('agy_p1_2');
    expect(result.current.edges.length).toBe(2);
  });

  it('ensures duplicate edge does NOT rename child', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c1' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(result.current.edges.length).toBe(1);

    // Duplicate attempt
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c1' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(result.current.edges.length).toBe(1);
    expect(countOutgoingEdges(result.current.edges, 'p1')).toBe(1);
  });

  it('rejects multi-parent child: target already has incoming edge', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    // Connect p1 -> c1 -> agy_p1_1
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'c1' });
    });
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(result.current.edges.length).toBe(1);

    // Attempt connecting p2 -> c1 (already has parent p1)
    act(() => {
      result.current.onConnect({ source: 'p2', target: 'c1' });
    });
    // c1 must NOT be renamed to agy_p2_1; must retain agy_p1_1
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('agy_p1_1');
    expect(result.current.edges.length).toBe(1);
  });

  it('ensures invalid edges (self, missing source, missing target) do NOT rename child', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    // Self connection
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'p1' });
    });
    expect(result.current.edges.length).toBe(0);
    expect(result.current.nodes.find((n) => n.id === 'p1').data.prefix).toBe('agy_p1');

    // Missing source
    act(() => {
      result.current.onConnect({ source: 'nonexistent', target: 'c1' });
    });
    expect(result.current.edges.length).toBe(0);
    expect(result.current.nodes.find((n) => n.id === 'c1').data.prefix).toBe('unlinked_1');

    // Missing target
    act(() => {
      result.current.onConnect({ source: 'p1', target: 'nonexistent' });
    });
    expect(result.current.edges.length).toBe(0);
    expect(countOutgoingEdges(result.current.edges, 'p1')).toBe(0);
  });

  it('supports functional setNodes and setEdges while maintaining consistency', () => {
    const { result } = renderHook(() => useProfileConnectionHarness(baseNodes));

    act(() => {
      result.current.setNodes((nds) =>
        nds.map((n) => (n.id === 'c1' ? { ...n, data: { ...n.data, customField: 42 } } : n))
      );
    });

    expect(result.current.nodes.find((n) => n.id === 'c1').data.customField).toBe(42);

    act(() => {
      result.current.setEdges((eds) => [...eds, { id: 'manual-1', source: 'p1', target: 'c4' }]);
    });

    expect(result.current.edges.length).toBe(1);
  });
});
