import React from 'react';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import ProfileGraph from './ProfileGraph';

// Mock resize observer for JSDOM
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

describe('ProfileGraph Component and onConnect Integration', () => {
  const initialNodes = [
    {
      id: 'p1',
      type: 'profile',
      position: { x: 100, y: 50 },
      data: { prefix: 'agy_p1', label: 'agy_p1', isRoot: true },
    },
    {
      id: 'p2',
      type: 'profile',
      position: { x: 300, y: 50 },
      data: { prefix: 'agy_p2', label: 'agy_p2', isRoot: true },
    },
    {
      id: 'c1',
      type: 'profile',
      position: { x: 100, y: 250 },
      data: { prefix: 'unlinked_1', label: 'unlinked_1', isRoot: false },
    },
    {
      id: 'c2',
      type: 'profile',
      position: { x: 300, y: 250 },
      data: { prefix: 'unlinked_2', label: 'unlinked_2', isRoot: false },
    },
  ];

  it('renders initial toolbar and controls', () => {
    render(<ProfileGraph initialNodes={initialNodes} />);
    expect(screen.getByText('Visual Profile Prefix Topology')).toBeTruthy();
    expect(screen.getByText('Editor Only — Local Graph')).toBeTruthy();
    expect(screen.getByTestId('btn-add-root')).toBeTruthy();
    expect(screen.getByTestId('btn-add-child')).toBeTruthy();
  });

  it('invokes connect and verifies child prefix is updated in the rendered DOM', () => {
    const graphRef = React.createRef();
    render(<ProfileGraph ref={graphRef} initialNodes={initialNodes} />);

    // Initially unlinked_1 is in the DOM
    expect(screen.getByText('unlinked_1')).toBeTruthy();

    // Invoke connect via component imperative handle
    act(() => {
      graphRef.current.connect({ source: 'p1', target: 'c1' });
    });

    // Child c1 label in the DOM should now be agy_p1_1
    expect(screen.getByText('agy_p1_1')).toBeTruthy();
    expect(screen.queryByText('unlinked_1')).toBeNull();

    // Connect c2 to agy_p1 -> should be agy_p1_2
    act(() => {
      graphRef.current.connect({ source: 'p1', target: 'c2' });
    });
    expect(screen.getByText('agy_p1_2')).toBeTruthy();
  });

  it('rejects duplicate and multi-parent connections in the rendered component', () => {
    const graphRef = React.createRef();
    render(<ProfileGraph ref={graphRef} initialNodes={initialNodes} />);

    // Connect p1 to c1 -> agy_p1_1
    act(() => {
      graphRef.current.connect({ source: 'p1', target: 'c1' });
    });
    expect(screen.getByText('agy_p1_1')).toBeTruthy();

    // Duplicate connect attempt p1 to c1
    act(() => {
      graphRef.current.connect({ source: 'p1', target: 'c1' });
    });
    expect(screen.getByText('agy_p1_1')).toBeTruthy();
    expect(graphRef.current.getEdges().length).toBe(1);

    // Multi-parent connect attempt: try connecting p2 to c1 (already has parent p1)
    act(() => {
      graphRef.current.connect({ source: 'p2', target: 'c1' });
    });
    // c1 MUST retain agy_p1_1 and NOT be renamed to agy_p2_1
    expect(screen.getByText('agy_p1_1')).toBeTruthy();
    expect(screen.queryByText('agy_p2_1')).toBeNull();
    expect(graphRef.current.getEdges().length).toBe(1);
  });

  it('derives outgoingCount dynamically from edges without stale state', () => {
    const graphRef = React.createRef();
    render(<ProfileGraph ref={graphRef} initialNodes={initialNodes} />);

    // Both p1 and p2 initially have 0 children
    expect(screen.getAllByText('0 children').length).toBe(2);

    // Connect p1 -> c1
    act(() => {
      graphRef.current.connect({ source: 'p1', target: 'c1' });
    });
    expect(screen.getByText('1 child')).toBeTruthy();
    expect(screen.getByText('0 children')).toBeTruthy(); // p2 still has 0

    // Connect p1 -> c2
    act(() => {
      graphRef.current.connect({ source: 'p1', target: 'c2' });
    });
    expect(screen.getByText('2 children')).toBeTruthy();
    expect(screen.getByText('0 children')).toBeTruthy(); // p2 still has 0
  });

  it('adds root nodes with unique IDs and auto-increment labels', () => {
    render(<ProfileGraph initialNodes={initialNodes} />);

    const input = screen.getByTestId('input-custom-prefix');
    const addRootBtn = screen.getByTestId('btn-add-root');

    // Add custom prefix root
    fireEvent.change(input, { target: { value: 'custom_pool_alpha' } });
    fireEvent.click(addRootBtn);
    expect(screen.getByText('custom_pool_alpha')).toBeTruthy();

    // Rapid clicks to add auto-increment roots without collisions
    fireEvent.click(addRootBtn);
    fireEvent.click(addRootBtn);

    expect(screen.getByText('agy_p3')).toBeTruthy();
    expect(screen.getByText('agy_p4')).toBeTruthy();
  });

  it('adds child nodes with collision-resistant IDs and sequential labels', () => {
    render(<ProfileGraph initialNodes={initialNodes} />);
    const addChildBtn = screen.getByTestId('btn-add-child');

    // Rapid clicks
    fireEvent.click(addChildBtn);
    fireEvent.click(addChildBtn);

    expect(screen.getByText('unlinked_3')).toBeTruthy();
    expect(screen.getByText('unlinked_4')).toBeTruthy();
  });

  it('resets graph restoring initial props instead of hardcoded default state', () => {
    const customInitialNodes = [
      {
        id: 'custom_prop_root',
        type: 'profile',
        position: { x: 50, y: 50 },
        data: { prefix: 'prop_root_1', label: 'prop_root_1', isRoot: true },
      },
    ];

    render(<ProfileGraph initialNodes={customInitialNodes} initialEdges={[]} />);
    expect(screen.getByText('prop_root_1')).toBeTruthy();

    // Add extra child
    const addChildBtn = screen.getByTestId('btn-add-child');
    fireEvent.click(addChildBtn);
    expect(screen.getByText('unlinked_1')).toBeTruthy();

    // Reset graph
    const resetBtn = screen.getByTestId('btn-reset-graph');
    fireEvent.click(resetBtn);

    // Initial prop root is preserved, added child is removed
    expect(screen.getByText('prop_root_1')).toBeTruthy();
    expect(screen.queryByText('unlinked_1')).toBeNull();
  });
});
