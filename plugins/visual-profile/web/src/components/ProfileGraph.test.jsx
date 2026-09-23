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

  it('updates prefix and dirty stats atomically when inline editing in ProfileGraph', () => {
    const realNodes = [
      {
        id: 'acc1.json',
        type: 'profile',
        position: { x: 50, y: 50 },
        data: {
          prefix: 'agy_p1',
          initialPrefix: 'agy_p1',
          label: 'agy_p1',
          fileName: 'acc1.json',
          isRoot: true,
          isDirty: false,
        },
      },
    ];

    render(<ProfileGraph initialNodes={realNodes} initialEdges={[]} />);

    expect(screen.getByText('agy_p1')).toBeTruthy();
    expect(screen.queryByTestId('stats-dirty-count')).toBeNull();

    // Start inline editing
    const editBtn = screen.getByTestId('btn-edit-prefix-acc1.json');
    fireEvent.click(editBtn);

    const input = screen.getByTestId('input-prefix-acc1.json');
    fireEvent.change(input, { target: { value: 'custom_edited_pool' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

    // Label and prefix updated in DOM
    expect(screen.getByText('custom_edited_pool')).toBeTruthy();
    expect(screen.getByTestId('badge-dirty-acc1.json')).toBeTruthy();
    expect(screen.getByTestId('stats-dirty-count')).toBeTruthy();
    expect(screen.getByTestId('stats-dirty-count').textContent).toContain('1');
  });

  it('updates child dirty state and stats counter when connecting real nodes', () => {
    const graphRef = React.createRef();
    const realNodes = [
      {
        id: 'root.json',
        type: 'profile',
        position: { x: 50, y: 50 },
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
        type: 'profile',
        position: { x: 50, y: 250 },
        data: {
          prefix: '',
          initialPrefix: '',
          label: 'child.json',
          isRoot: false,
          isDirty: false,
        },
      },
    ];

    render(<ProfileGraph ref={graphRef} initialNodes={realNodes} initialEdges={[]} />);

    act(() => {
      graphRef.current.connect({ source: 'root.json', target: 'child.json' });
    });

    expect(screen.getByText('agy_p1_1')).toBeTruthy();
    expect(screen.getByTestId('badge-dirty-child.json')).toBeTruthy();
    expect(screen.getByTestId('stats-dirty-count')).toBeTruthy();
  });

  it('hides synthetic creation buttons when allowSynthetic is false (production mode)', () => {
    render(<ProfileGraph initialNodes={initialNodes} allowSynthetic={false} />);

    expect(screen.getByTestId('badge-live-auth')).toBeTruthy();
    expect(screen.queryByTestId('btn-add-root')).toBeNull();
    expect(screen.queryByTestId('btn-add-child')).toBeNull();
    expect(screen.queryByTestId('input-custom-prefix')).toBeNull();
    // Reset graph button remains
    expect(screen.getByTestId('btn-reset-graph')).toBeTruthy();
  });

  it('prompts confirmation when resetting graph with dirty edits and aborts if user cancels', () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);

    const realNodes = [
      {
        id: 'acc1.json',
        type: 'profile',
        position: { x: 50, y: 50 },
        data: {
          prefix: 'agy_p1',
          initialPrefix: 'agy_p1',
          label: 'agy_p1',
          fileName: 'acc1.json',
          isRoot: true,
          isDirty: false,
        },
      },
    ];

    const graphRef = React.createRef();
    render(<ProfileGraph ref={graphRef} initialNodes={realNodes} initialEdges={[]} />);

    // Make node dirty via edit
    fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
    const input = screen.getByTestId('input-prefix-acc1.json');
    fireEvent.change(input, { target: { value: 'dirty_prefix' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

    expect(screen.getByText('dirty_prefix')).toBeTruthy();
    expect(graphRef.current.isDirty()).toBe(true);

    // Click Reset: confirm is called and returns false
    fireEvent.click(screen.getByTestId('btn-reset-graph'));
    expect(confirmSpy).toHaveBeenCalled();

    // Node is STILL dirty and modification is preserved
    expect(screen.getByText('dirty_prefix')).toBeTruthy();
    expect(graphRef.current.isDirty()).toBe(true);

    // Now confirm reset
    confirmSpy.mockReturnValue(true);
    fireEvent.click(screen.getByTestId('btn-reset-graph'));

    // Node is reset back to initial prefix
    expect(screen.getByText('agy_p1')).toBeTruthy();
    expect(graphRef.current.isDirty()).toBe(false);
  });

  describe('VP-4 Explicit Save Changes, PATCH, Reconciliation, and Partial Failure', () => {
    it('keeps local-only demo mode strictly non-persistable', async () => {
      const graphRef = React.createRef();
      render(<ProfileGraph ref={graphRef} initialNodes={initialNodes} allowSynthetic={true} />);

      // Save button must NOT be rendered in demo mode
      expect(screen.queryByTestId('btn-save-changes')).toBeNull();

      // Programmatic save attempt must reject
      let result;
      await act(async () => {
        result = await graphRef.current.save();
      });
      expect(result.success).toBe(false);
      expect(result.error).toContain('Demo mode is local-only and non-persistable');
    });

    it('enables Save button only when real dirty nodes exist in live mode', () => {
      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'agy_p1',
            initialPrefix: 'agy_p1',
            label: 'agy_p1',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      render(<ProfileGraph initialNodes={realNodes} initialEdges={[]} allowSynthetic={false} />);

      const saveBtn = screen.getByTestId('btn-save-changes');
      expect(saveBtn).toBeTruthy();
      expect(saveBtn.disabled).toBe(true);

      // Edit node prefix to make it dirty
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: 'agy_edited' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

      // Save button should now be enabled and show dirty count
      expect(saveBtn.disabled).toBe(false);
      expect(saveBtn.textContent).toContain('Save Changes (1)');
    });

    it('sends PATCH with exact physical filename and prefix, including empty string', async () => {
      const capturedRequests = [];
      const mockFetch = vi.fn().mockImplementation((url, opts) => {
        capturedRequests.push({ url, opts, body: JSON.parse(opts.body) });
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ status: 'ok' }),
        });
      });

      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'agy_p1',
            initialPrefix: 'agy_p1',
            label: 'agy_p1',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      render(
        <ProfileGraph
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'mgmt-key-1', fetchFn: mockFetch }}
        />
      );

      // Edit to empty string prefix
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: '' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

      // Save changes
      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      expect(capturedRequests.length).toBe(1);
      expect(capturedRequests[0].url).toBe('/v0/management/auth-files/fields');
      expect(capturedRequests[0].opts.method).toBe('PATCH');
      expect(capturedRequests[0].opts.headers['Authorization']).toBe('Bearer mgmt-key-1');
      expect(capturedRequests[0].body).toEqual({
        name: 'acc1.json',
        prefix: '',
      });

      // Feedback banner confirms success
      expect(screen.getByTestId('save-status-success')).toBeTruthy();
      expect(screen.getByText('Successfully saved 1 profile(s).')).toBeTruthy();
    });

    it('never sends synthetic nodes to the Management API', async () => {
      const capturedNames = [];
      const mockFetch = vi.fn().mockImplementation((url, opts) => {
        const body = JSON.parse(opts.body);
        capturedNames.push(body.name);
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ status: 'ok' }),
        });
      });

      const mixedNodes = [
        {
          id: 'real-auth.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'p_real_dirty',
            initialPrefix: 'p_real_init',
            label: 'real-auth.json',
            fileName: 'real-auth.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
        {
          id: 'synthetic-node',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            prefix: 'p_synthetic_dirty',
            initialPrefix: 'p_synthetic_init',
            label: 'synthetic-node',
            fileName: 'synthetic-node',
            isRoot: false,
            isDirty: true,
            isSynthetic: true, // Marked synthetic
          },
        },
      ];

      render(
        <ProfileGraph
          initialNodes={mixedNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      expect(capturedNames).toEqual(['real-auth.json']);
      expect(capturedNames.includes('synthetic-node')).toBe(false);
    });

    it('prevents overlapping saves while a request is in flight', async () => {
      let resolveFetch;
      const pendingPromise = new Promise((resolve) => {
        resolveFetch = resolve;
      });

      const mockFetch = vi.fn().mockReturnValue(
        pendingPromise.then(() => ({
          ok: true,
          status: 200,
          json: async () => ({ status: 'ok' }),
        }))
      );

      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'p1_dirty',
            initialPrefix: 'p1_init',
            label: 'p1_dirty',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Trigger first save
      act(() => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      expect(graphRef.current.isSaving()).toBe(true);
      expect(screen.getByText('Saving...')).toBeTruthy();

      // Trigger second overlapping save while first is pending
      const overlapResult = await graphRef.current.save();
      expect(overlapResult.success).toBe(false);
      expect(overlapResult.error).toBe('Save already in progress');

      // mockFetch must have been called exactly ONCE
      expect(mockFetch).toHaveBeenCalledTimes(1);

      // Finish first request
      await act(async () => {
        resolveFetch();
      });

      expect(graphRef.current.isSaving()).toBe(false);
      expect(screen.getByTestId('save-status-success')).toBeTruthy();
    });

    it('CRITICAL: does not lose edits made while request is pending and preserves latest draft', async () => {
      let resolveFetch;
      const pendingPromise = new Promise((resolve) => {
        resolveFetch = resolve;
      });

      const mockFetch = vi.fn().mockReturnValue(
        pendingPromise.then(() => ({
          ok: true,
          status: 200,
          json: async () => ({ status: 'ok' }),
        }))
      );

      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'submitted_prefix',
            initialPrefix: 'original_prefix',
            label: 'submitted_prefix',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // 1. Click Save Changes: request for 'submitted_prefix' is sent and is in flight
      act(() => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });
      expect(graphRef.current.isSaving()).toBe(true);

      // 2. While request is pending, user makes another draft modification
      act(() => {
        graphRef.current.updateNodePrefix('acc1.json', 'concurrent_draft_prefix');
      });

      // Verify draft prefix is immediately in DOM
      expect(screen.getByText('concurrent_draft_prefix')).toBeTruthy();

      // 3. Network response arrives successfully for 'submitted_prefix'
      await act(async () => {
        resolveFetch();
      });

      expect(graphRef.current.isSaving()).toBe(false);

      // 4. Verification:
      // Draft prefix 'concurrent_draft_prefix' MUST NOT be overwritten or lost!
      expect(screen.getByText('concurrent_draft_prefix')).toBeTruthy();
      // Node MUST remain dirty because concurrent draft differs from the newly saved initialPrefix
      expect(graphRef.current.isDirty()).toBe(true);
      const node = graphRef.current.getNodes().find((n) => n.id === 'acc1.json');
      expect(node.data.prefix).toBe('concurrent_draft_prefix');
      expect(node.data.initialPrefix).toBe('submitted_prefix');
      expect(node.data.isDirty).toBe(true);
    });

    it('handles multi-file partial failure without claiming atomicity and allows retry', async () => {
      const mockFetch = vi.fn().mockImplementation((url, opts) => {
        const body = JSON.parse(opts.body);
        if (body.name === 'good.json') {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ status: 'ok' }),
          });
        }
        if (body.name === 'conflict.json') {
          return Promise.resolve({
            ok: false,
            status: 409,
            json: async () => ({ error: 'Conflict: duplicate prefix' }),
          });
        }
        return Promise.reject(new Error('Unknown'));
      });

      const realNodes = [
        {
          id: 'good.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'good_new',
            initialPrefix: 'good_old',
            label: 'good_new',
            fileName: 'good.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
        {
          id: 'conflict.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            prefix: 'conflict_new',
            initialPrefix: 'conflict_old',
            label: 'conflict_new',
            fileName: 'conflict.json',
            isRoot: false,
            isDirty: true,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Trigger Save Changes
      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      // Partial failure banner is shown
      expect(screen.getByTestId('save-status-partial')).toBeTruthy();
      expect(screen.getByText(/Partially saved: 1 succeeded, 1 failed/)).toBeTruthy();
      expect(screen.getByTestId('save-failure-conflict.json')).toBeTruthy();
      expect(screen.getByTestId('save-failure-conflict.json').textContent).toContain(
        '409 Conflict'
      );

      // Check node state reconciliation:
      const nodes = graphRef.current.getNodes();
      const goodNode = nodes.find((n) => n.id === 'good.json');
      const conflictNode = nodes.find((n) => n.id === 'conflict.json');

      // good.json advanced initialPrefix and cleared dirty
      expect(goodNode.data.initialPrefix).toBe('good_new');
      expect(goodNode.data.isDirty).toBe(false);

      // conflict.json retained original initialPrefix and remained dirty
      expect(conflictNode.data.initialPrefix).toBe('conflict_old');
      expect(conflictNode.data.isDirty).toBe(true);

      // Retry button is available
      const retryBtn = screen.getByTestId('btn-retry-save');
      expect(retryBtn).toBeTruthy();

      // Clear mock calls and configure conflict.json to succeed on retry
      mockFetch.mockClear();
      mockFetch.mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok' }),
      });

      // Click Retry
      await act(async () => {
        fireEvent.click(retryBtn);
      });

      // On retry, ONLY conflict.json is retried (good.json is already saved and clean)
      expect(mockFetch).toHaveBeenCalledTimes(1);
      const retryBody = JSON.parse(mockFetch.mock.calls[0][1].body);
      expect(retryBody.name).toBe('conflict.json');

      // Now all files succeeded
      expect(screen.getByTestId('save-status-success')).toBeTruthy();
      expect(graphRef.current.isDirty()).toBe(false);
    });

    it('sanitizes 401 Unauthorized and 404 Not Found errors without leaking secrets', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({ error: 'unauthorized' }),
      });

      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'p1_dirty',
            initialPrefix: 'p1_init',
            label: 'p1_dirty',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
      ];

      render(
        <ProfileGraph
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'super-sensitive-token-xyz', fetchFn: mockFetch }}
        />
      );

      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      expect(screen.getByTestId('save-status-error')).toBeTruthy();
      expect(screen.getByText(/401 Unauthorized/)).toBeTruthy();
      // Crucial: token is not leaked into DOM
      expect(screen.queryByText(/super-sensitive-token-xyz/)).toBeNull();
    });

    it('permits saving duplicate prefixes for accounts sharing a rotation pool', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ ok: true }),
      });

      const poolNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'duplicate_pool',
            initialPrefix: 'p1_init',
            label: 'duplicate_pool',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
        {
          id: 'acc2.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            prefix: 'duplicate_pool',
            initialPrefix: 'p2_init',
            label: 'duplicate_pool',
            fileName: 'acc2.json',
            isRoot: true,
            isDirty: true,
            isSynthetic: false,
          },
        },
      ];

      render(
        <ProfileGraph
          initialNodes={poolNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      // Saving duplicate prefixes in pool rotation must be permitted
      expect(mockFetch).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId('save-status-success')).toBeTruthy();
    });

    it('saved graph reset restores saved baseline rather than stale initial props', async () => {
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ ok: true }),
      });

      const initialNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'initial_val',
            initialPrefix: 'initial_val',
            label: 'initial_val',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={initialNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // 1. Edit prefix to saved_val and save
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: 'saved_val' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });
      expect(screen.getByTestId('save-status-success')).toBeTruthy();
      expect(graphRef.current.isDirty()).toBe(false);

      // 2. Make another dirty edit to dirty_val
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: 'dirty_val' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));
      expect(screen.getByText('dirty_val')).toBeTruthy();
      expect(graphRef.current.isDirty()).toBe(true);

      // 3. Reset graph: must restore saved_val (the saved baseline), NOT initial_val (stale prop)
      fireEvent.click(screen.getByTestId('btn-reset-graph'));

      expect(screen.getByText('saved_val')).toBeTruthy();
      expect(screen.queryByText('initial_val')).toBeNull();
      expect(graphRef.current.isDirty()).toBe(false);
    });

    it('prevents Reset Graph while a save is in progress', async () => {
      let resolveSave;
      const savePromise = new Promise((resolve) => {
        resolveSave = resolve;
      });
      const mockFetch = vi.fn().mockReturnValue(
        savePromise.then(() => ({
          ok: true,
          status: 200,
          json: async () => ({ ok: true }),
        }))
      );

      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'agy_p1',
            initialPrefix: 'agy_p1',
            label: 'agy_p1',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Edit to make dirty
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: 'in_flight' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

      // Start save
      act(() => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      // While saving, Reset button must be disabled and ref.reset() must return false
      expect(screen.getByTestId('btn-reset-graph').disabled).toBe(true);
      expect(graphRef.current.isSaving()).toBe(true);
      expect(graphRef.current.reset()).toBe(false);

      // Complete save
      await act(async () => {
        resolveSave();
      });

      expect(screen.getByTestId('btn-reset-graph').disabled).toBe(false);
      expect(graphRef.current.isSaving()).toBe(false);
    });

    it('does not send PATCH on keystroke (no auto-PATCH on keystroke)', () => {
      const mockFetch = vi.fn();

      const realNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'agy_p1',
            initialPrefix: 'agy_p1',
            label: 'agy_p1',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      render(
        <ProfileGraph
          initialNodes={realNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Open edit mode and type
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      const input = screen.getByTestId('input-prefix-acc1.json');
      fireEvent.change(input, { target: { value: 'keystroke_1' } });
      fireEvent.change(input, { target: { value: 'keystroke_2' } });

      // No network calls on keystrokes
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('regression: rerender saved graph preserves saved baseline and does not re-clobber to stale initial props on reset', async () => {
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ ok: true }),
      });

      const initialNodes = [
        {
          id: 'acc1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'orig_prefix',
            initialPrefix: 'orig_prefix',
            label: 'orig_prefix',
            fileName: 'acc1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      const { rerender } = render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={initialNodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Save a new prefix
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: 'saved_prefix' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));

      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });
      expect(screen.getByTestId('save-status-success')).toBeTruthy();

      // Parent rerenders with fresh reference to initialNodes
      rerender(
        <ProfileGraph
          ref={graphRef}
          initialNodes={[...initialNodes]}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Edit node again after rerender
      fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-acc1.json'), {
        target: { value: 'dirty_after_rerender' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));
      expect(screen.getByText('dirty_after_rerender')).toBeTruthy();

      // Reset graph: must NOT re-clobber to orig_prefix from initial props
      fireEvent.click(screen.getByTestId('btn-reset-graph'));

      expect(screen.getByText('saved_prefix')).toBeTruthy();
      expect(screen.queryByText('orig_prefix')).toBeNull();
      expect(graphRef.current.isDirty()).toBe(false);
    });

    it('regression: in-flight edge changes are captured in baseline during async save', async () => {
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      let resolveSave;
      const savePromise = new Promise((resolve) => {
        resolveSave = resolve;
      });
      const mockFetch = vi.fn().mockReturnValue(
        savePromise.then(() => ({
          ok: true,
          status: 200,
          json: async () => ({ ok: true }),
        }))
      );

      const nodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'p1_init',
            initialPrefix: 'p1_init',
            label: 'p1_init',
            fileName: 'p1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            prefix: '',
            initialPrefix: '',
            label: 'c1.json',
            fileName: 'c1.json',
            isRoot: false,
            isDirty: false,
            isSynthetic: false,
          },
        },
        {
          id: 'other.json',
          type: 'profile',
          position: { x: 250, y: 50 },
          data: {
            prefix: 'other_init',
            initialPrefix: 'other_init',
            label: 'other_init',
            fileName: 'other.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={nodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Edit p1 and trigger save
      fireEvent.click(screen.getByTestId('btn-edit-prefix-p1.json'));
      fireEvent.change(screen.getByTestId('input-prefix-p1.json'), {
        target: { value: 'p1_saved' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-p1.json'));

      act(() => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });
      expect(graphRef.current.isSaving()).toBe(true);

      // WHILE SAVE IS IN FLIGHT: connect p1 -> c1
      act(() => {
        graphRef.current.connect({ source: 'p1.json', target: 'c1.json' });
      });
      expect(graphRef.current.getEdges().length).toBe(1);

      // Resolve save
      await act(async () => {
        resolveSave();
      });
      expect(graphRef.current.isSaving()).toBe(false);

      // Make a temporary prefix edit on other.json
      fireEvent.click(screen.getByTestId('btn-edit-prefix-other.json'));
      fireEvent.change(screen.getByTestId('input-prefix-other.json'), {
        target: { value: 'other_dirty' },
      });
      fireEvent.click(screen.getByTestId('btn-save-prefix-other.json'));
      expect(screen.getByText('other_dirty')).toBeTruthy();
      expect(graphRef.current.isDirty()).toBe(true);

      // Reset graph: baseline MUST retain the in-flight edge connection p1 -> c1
      fireEvent.click(screen.getByTestId('btn-reset-graph'));

      expect(graphRef.current.getEdges().length).toBe(1);
      expect(graphRef.current.getEdges()[0].source).toBe('p1.json');
      expect(graphRef.current.getEdges()[0].target).toBe('c1.json');
      expect(screen.getByText('p1_saved')).toBeTruthy();
      expect(screen.getByText('other_init')).toBeTruthy();
      expect(screen.queryByText('other_dirty')).toBeNull();
    });

    it('regression: in-flight disconnect while save is pending preserves prefix and edge coherence in baseline upon reset', async () => {
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      let resolveSave;
      const savePromise = new Promise((resolve) => {
        resolveSave = resolve;
      });
      const mockFetch = vi.fn().mockReturnValue(
        savePromise.then(() => ({
          ok: true,
          status: 200,
          json: async () => ({ ok: true }),
        }))
      );

      const nodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            prefix: 'agy_p1',
            initialPrefix: 'agy_p1',
            label: 'agy_p1',
            fileName: 'p1.json',
            isRoot: true,
            isDirty: false,
            isSynthetic: false,
          },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            prefix: '',
            initialPrefix: '',
            label: 'c1.json',
            fileName: 'c1.json',
            isRoot: false,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={nodes}
          initialEdges={[]}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Connect p1 to c1 -> assigns prefix agy_p1_1, marks isDirty: true
      act(() => {
        graphRef.current.connect({ source: 'p1.json', target: 'c1.json' });
      });
      expect(graphRef.current.getEdges()).toHaveLength(1);
      expect(graphRef.current.getNodes().find((n) => n.id === 'c1.json').data.prefix).toBe('agy_p1_1');
      expect(graphRef.current.isDirty()).toBe(true);

      // Trigger Save Changes (persisting prefix agy_p1_1 for c1.json)
      act(() => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });
      expect(graphRef.current.isSaving()).toBe(true);

      // WHILE SAVE IS IN FLIGHT: disconnect child c1.json via Disconnect button
      fireEvent.click(screen.getByTestId('btn-disconnect-c1.json'));
      // In active draft, c1 is disconnected and prefix is cleared
      expect(graphRef.current.getEdges()).toHaveLength(0);
      expect(graphRef.current.getNodes().find((n) => n.id === 'c1.json').data.prefix).toBe('');

      // Resolve the async save
      await act(async () => {
        resolveSave();
      });
      expect(graphRef.current.isSaving()).toBe(false);

      // In active graph after save, draft disconnect is preserved:
      // c1 still has draft prefix "" and isDirty true (against new server initialPrefix agy_p1_1)
      const draftChild = graphRef.current.getNodes().find((n) => n.id === 'c1.json');
      expect(draftChild.data.prefix).toBe('');
      expect(draftChild.data.initialPrefix).toBe('agy_p1_1');
      expect(draftChild.data.isDirty).toBe(true);
      expect(graphRef.current.getEdges()).toHaveLength(0);

      // Reset graph: baseline MUST restore the coherent server-saved state (both prefix AND edge)
      fireEvent.click(screen.getByTestId('btn-reset-graph'));

      // Verify prefix/edge coherence: child prefix agy_p1_1 MUST have its incoming parent edge p1 -> c1
      expect(graphRef.current.getEdges()).toHaveLength(1);
      expect(graphRef.current.getEdges()[0].source).toBe('p1.json');
      expect(graphRef.current.getEdges()[0].target).toBe('c1.json');

      const resetChild = graphRef.current.getNodes().find((n) => n.id === 'c1.json');
      expect(resetChild.data.prefix).toBe('agy_p1_1');
      expect(resetChild.data.initialPrefix).toBe('agy_p1_1');
      expect(resetChild.data.isDirty).toBe(false);
      expect(graphRef.current.isDirty()).toBe(false);
    });
  });

  describe('VP-7 Edge Disconnect, Descendant Guard, and Empty Prefix Save Integration', () => {
    it('disconnects child via visible Disconnect button, clearing prefix to empty and marking isDirty', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: {
            fileName: 'p1.json',
            prefix: 'agy_p1',
            initialPrefix: 'agy_p1',
            label: 'agy_p1',
            isRoot: true,
            isDirty: false,
          },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            fileName: 'c1.json',
            prefix: 'agy_p1_1',
            initialPrefix: 'agy_p1_1',
            label: 'agy_p1_1',
            isRoot: false,
            isDirty: false,
          },
        },
      ];
      const realEdges = [
        { id: 'edge__p1.json-c1.json', source: 'p1.json', target: 'c1.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      // Child has visible Disconnect button
      const disconnectBtn = screen.getByTestId('btn-disconnect-c1.json');
      expect(disconnectBtn).toBeTruthy();

      fireEvent.click(disconnectBtn);

      // Edge is removed atomically
      expect(graphRef.current.getEdges()).toHaveLength(0);

      // Child prefix cleared, isDirty set to true
      const c1 = graphRef.current.getNodes().find((n) => n.id === 'c1.json');
      expect(c1.data.prefix).toBe('');
      expect(c1.data.label).toBe('c1.json');
      expect(c1.data.isDirty).toBe(true);
      expect(graphRef.current.isDirty()).toBe(true);
      expect(screen.getByTestId('stats-dirty-count').textContent).toContain('1');
    });

    it('removes edge via removeEdges imperative handle and clears child prefix to empty', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: { fileName: 'c1.json', prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e1', source: 'p1.json', target: 'c1.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      act(() => {
        graphRef.current.removeEdges(['e1']);
      });

      expect(graphRef.current.getEdges()).toHaveLength(0);
      const c1 = graphRef.current.getNodes().find((n) => n.id === 'c1.json');
      expect(c1.data.prefix).toBe('');
      expect(c1.data.isDirty).toBe(true);
    });

    it('blocks disconnect and shows feedback banner when child has attached descendants', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 200 },
          data: { fileName: 'c1.json', prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', isRoot: false },
        },
        {
          id: 'c2.json',
          type: 'profile',
          position: { x: 50, y: 350 },
          data: { fileName: 'c2.json', prefix: 'agy_p1_1_1', initialPrefix: 'agy_p1_1_1', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e1', source: 'p1.json', target: 'c1.json' },
        { id: 'e2', source: 'c1.json', target: 'c2.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      // Attempting to disconnect c1 via button
      const disconnectBtn = screen.getByTestId('btn-disconnect-c1.json');
      fireEvent.click(disconnectBtn);

      // Guard blocks: feedback banner is rendered
      expect(screen.getByTestId('validation-error-c1.json')).toBeTruthy();
      expect(screen.getByText(/Cannot disconnect profile with attached descendants/)).toBeTruthy();

      // State is preserved completely
      expect(graphRef.current.getEdges()).toHaveLength(2);
      expect(graphRef.current.getNodes().find((n) => n.id === 'c1.json').data.prefix).toBe('agy_p1_1');
    });

    it('removeEdges batch rejects guarded edge while allowing leaf edge', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'c_mid.json',
          type: 'profile',
          position: { x: 50, y: 200 },
          data: { fileName: 'c_mid.json', prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', isRoot: false },
        },
        {
          id: 'c_deep.json',
          type: 'profile',
          position: { x: 50, y: 350 },
          data: { fileName: 'c_deep.json', prefix: 'agy_p1_1_1', initialPrefix: 'agy_p1_1_1', isRoot: false },
        },
        {
          id: 'c_leaf.json',
          type: 'profile',
          position: { x: 250, y: 200 },
          data: { fileName: 'c_leaf.json', prefix: 'agy_p1_2', initialPrefix: 'agy_p1_2', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e_guarded', source: 'p1.json', target: 'c_mid.json' },
        { id: 'e_child', source: 'c_mid.json', target: 'c_deep.json' },
        { id: 'e_leaf', source: 'p1.json', target: 'c_leaf.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      // Attempt batch removal of e_guarded and e_leaf
      act(() => {
        graphRef.current.removeEdges(['e_guarded', 'e_leaf']);
      });

      // e_leaf is safely removed; e_guarded is preserved because c_mid still has e_child
      const edges = graphRef.current.getEdges();
      expect(edges.some((e) => e.id === 'e_leaf')).toBe(false);
      expect(edges.some((e) => e.id === 'e_guarded')).toBe(true);

      // c_leaf prefix cleared
      expect(graphRef.current.getNodes().find((n) => n.id === 'c_leaf.json').data.prefix).toBe('');
      // c_mid prefix preserved
      expect(graphRef.current.getNodes().find((n) => n.id === 'c_mid.json').data.prefix).toBe('agy_p1_1');

      // Feedback banner rendered for rejected removal
      expect(screen.getByTestId('graph-feedback-banner')).toBeTruthy();
      expect(screen.getByText(/Cannot disconnect profile\(s\) \[c_mid\.json\]/)).toBeTruthy();
    });

    it('reconnecting child to same parent after disconnect resets isDirty to false', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: { fileName: 'c1.json', prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e1', source: 'p1.json', target: 'c1.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      // 1. Disconnect child
      fireEvent.click(screen.getByTestId('btn-disconnect-c1.json'));
      expect(graphRef.current.isDirty()).toBe(true);

      // 2. Reconnect child to same parent p1.json
      act(() => {
        graphRef.current.connect({ source: 'p1.json', target: 'c1.json' });
      });

      // Prefix is restored to agy_p1_1 matching initialPrefix, so isDirty resets to false!
      const c1 = graphRef.current.getNodes().find((n) => n.id === 'c1.json');
      expect(c1.data.prefix).toBe('agy_p1_1');
      expect(c1.data.isDirty).toBe(false);
      expect(graphRef.current.isDirty()).toBe(false);
    });

    it('explicit Save Changes persists empty prefix via PATCH {name, prefix: ""} and reconciles initialPrefix', async () => {
      let capturedPayload = null;
      const mockFetch = vi.fn().mockImplementation((url, opts) => {
        capturedPayload = JSON.parse(opts.body);
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ status: 'ok' }),
        });
      });

      const realNodes = [
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 250 },
          data: {
            fileName: 'c1.json',
            prefix: 'agy_p1_1',
            initialPrefix: 'agy_p1_1',
            label: 'agy_p1_1',
            isRoot: false,
            isDirty: false,
            isSynthetic: false,
          },
        },
      ];
      const realEdges = [
        { id: 'e1', source: 'p1.json', target: 'c1.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
          apiOptions={{ key: 'test-key', fetchFn: mockFetch }}
        />
      );

      // Disconnect child
      fireEvent.click(screen.getByTestId('btn-disconnect-c1.json'));
      expect(graphRef.current.isDirty()).toBe(true);
      expect(graphRef.current.getNodes().find((n) => n.id === 'c1.json').data.prefix).toBe('');

      // Click explicit Save Changes button
      await act(async () => {
        fireEvent.click(screen.getByTestId('btn-save-changes'));
      });

      // Verify exact payload sent to Management API PATCH
      expect(capturedPayload).toEqual({
        name: 'c1.json',
        prefix: '',
      });

      // Verify node state after reconciliation: initialPrefix is now '', isDirty is false
      expect(graphRef.current.isDirty()).toBe(false);
      const c1 = graphRef.current.getNodes().find((n) => n.id === 'c1.json');
      expect(c1.data.initialPrefix).toBe('');
      expect(c1.data.prefix).toBe('');
      expect(c1.data.isDirty).toBe(false);
      expect(screen.getByTestId('save-status-success')).toBeTruthy();
    });

    it('masks email addresses in graph feedback banner when edge removal is rejected', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'developer.user@openai.com.json',
          type: 'profile',
          position: { x: 50, y: 200 },
          data: {
            fileName: 'developer.user@openai.com.json',
            prefix: 'agy_p1_1',
            initialPrefix: 'agy_p1_1',
            isRoot: false,
          },
        },
        {
          id: 'c_deep.json',
          type: 'profile',
          position: { x: 50, y: 350 },
          data: { fileName: 'c_deep.json', prefix: 'agy_p1_1_1', initialPrefix: 'agy_p1_1_1', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e_guarded', source: 'p1.json', target: 'developer.user@openai.com.json' },
        { id: 'e_child', source: 'developer.user@openai.com.json', target: 'c_deep.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      act(() => {
        graphRef.current.removeEdges(['e_guarded']);
      });

      // Feedback banner rendered with masked email identifier
      const banner = screen.getByTestId('graph-feedback-banner');
      expect(banner).toBeTruthy();
      expect(screen.getByText(/Cannot disconnect profile\(s\) \[dev\.\.\.ser@openai\.com\.json\]/)).toBeTruthy();

      // Crucial: verify raw unmasked email is not in the banner content
      expect(banner.textContent).not.toContain('developer.user@openai.com.json');
    });

    it('clears stale graph feedback banner automatically upon subsequent successful action', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'c_mid.json',
          type: 'profile',
          position: { x: 50, y: 200 },
          data: { fileName: 'c_mid.json', prefix: 'agy_p1_1', isRoot: false },
        },
        {
          id: 'c_leaf.json',
          type: 'profile',
          position: { x: 50, y: 350 },
          data: { fileName: 'c_leaf.json', prefix: 'agy_p1_1_1', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e_mid', source: 'p1.json', target: 'c_mid.json' },
        { id: 'e_leaf', source: 'c_mid.json', target: 'c_leaf.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      // 1. Attempt invalid removal of e_mid while c_leaf is attached -> banner appears
      act(() => {
        graphRef.current.removeEdges(['e_mid']);
      });
      expect(screen.getByTestId('graph-feedback-banner')).toBeTruthy();

      // 2. Perform a successful removal of e_leaf -> banner is automatically cleared!
      act(() => {
        graphRef.current.removeEdges(['e_leaf']);
      });
      expect(screen.queryByTestId('graph-feedback-banner')).toBeNull();
      expect(graphRef.current.getEdges().some((e) => e.id === 'e_leaf')).toBe(false);
    });

    it('handles batched rapid disconnections without stale closure false rejections', () => {
      const realNodes = [
        {
          id: 'p1.json',
          type: 'profile',
          position: { x: 50, y: 50 },
          data: { fileName: 'p1.json', prefix: 'agy_p1', initialPrefix: 'agy_p1', isRoot: true },
        },
        {
          id: 'c1.json',
          type: 'profile',
          position: { x: 50, y: 200 },
          data: { fileName: 'c1.json', prefix: 'agy_p1_1', initialPrefix: 'agy_p1_1', isRoot: false },
        },
        {
          id: 'c2.json',
          type: 'profile',
          position: { x: 50, y: 350 },
          data: { fileName: 'c2.json', prefix: 'agy_p1_1_1', initialPrefix: 'agy_p1_1_1', isRoot: false },
        },
      ];
      const realEdges = [
        { id: 'e1', source: 'p1.json', target: 'c1.json' },
        { id: 'e2', source: 'c1.json', target: 'c2.json' },
      ];

      const graphRef = React.createRef();
      render(
        <ProfileGraph
          ref={graphRef}
          initialNodes={realNodes}
          initialEdges={realEdges}
          allowSynthetic={false}
        />
      );

      // Rapidly disconnect c2 then c1 without waiting for full re-render cycles
      act(() => {
        graphRef.current.disconnectNode('c2.json');
        graphRef.current.disconnectNode('c1.json');
      });

      // Both edges must be removed safely; c1 must NOT be falsely rejected by stale closure
      expect(graphRef.current.getEdges()).toHaveLength(0);
      expect(graphRef.current.getNodes().find((n) => n.id === 'c1.json').data.prefix).toBe('');
      expect(graphRef.current.getNodes().find((n) => n.id === 'c2.json').data.prefix).toBe('');
      expect(screen.queryByTestId('graph-feedback-banner')).toBeNull();
    });
  });
});
