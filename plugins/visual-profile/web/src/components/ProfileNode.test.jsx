import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { ReactFlowProvider } from '@xyflow/react';
import ProfileNode, { validateNodePrefix } from './ProfileNode';

function renderInProvider(ui) {
  return render(<ReactFlowProvider>{ui}</ReactFlowProvider>);
}

describe('validateNodePrefix', () => {
  it('allows exact empty string per Management API', () => {
    expect(validateNodePrefix('')).toEqual({ valid: true, value: '' });
    expect(validateNodePrefix('   ')).toEqual({ valid: true, value: '' });
    expect(validateNodePrefix(null)).toEqual({ valid: true, value: '' });
  });

  it('validates standard prefixes with letters, numbers, underscores, and dashes', () => {
    expect(validateNodePrefix('agy_p1')).toEqual({ valid: true, value: 'agy_p1' });
    expect(validateNodePrefix('custom_pool-prod_01')).toEqual({
      valid: true,
      value: 'custom_pool-prod_01',
    });
  });

  it('rejects invalid prefixes with spaces or special characters', () => {
    const spaceResult = validateNodePrefix('prefix with spaces');
    expect(spaceResult.valid).toBe(false);
    expect(spaceResult.error).toBeTruthy();

    const charResult = validateNodePrefix('invalid@prefix!');
    expect(charResult.valid).toBe(false);
  });
});

describe('ProfileNode Component - Inline Editing & Dirty State', () => {
  it('renders node prefix and badges', () => {
    renderInProvider(
      <ProfileNode
        id="node1"
        data={{ prefix: 'agy_p1', label: 'agy_p1', isRoot: true, isDirty: false }}
        isConnectable={true}
      />
    );

    expect(screen.getByText('agy_p1')).toBeTruthy();
    expect(screen.getByText('ROOT')).toBeTruthy();
    expect(screen.queryByTestId('badge-dirty-node1')).toBeNull();
  });

  it('displays DIRTY badge when node is dirty', () => {
    renderInProvider(
      <ProfileNode
        id="node2"
        data={{ prefix: 'agy_p2_1', label: 'agy_p2_1', isRoot: false, isDirty: true }}
        isConnectable={true}
      />
    );

    expect(screen.getByTestId('badge-dirty-node2')).toBeTruthy();
    expect(screen.getByText('DIRTY')).toBeTruthy();
    expect(screen.getByText('CHILD')).toBeTruthy();
  });

  it('enters inline edit mode with nopan and nodrag classes', () => {
    renderInProvider(
      <ProfileNode
        id="node1"
        data={{ prefix: 'agy_p1', label: 'agy_p1', isRoot: true }}
        isConnectable={true}
      />
    );

    const editBtn = screen.getByTestId('btn-edit-prefix-node1');
    fireEvent.click(editBtn);

    const input = screen.getByTestId('input-prefix-node1');
    expect(input).toBeTruthy();
    expect(input.classList.contains('nodrag')).toBe(true);
    expect(input.classList.contains('nopan')).toBe(true);
    expect(input.value).toBe('agy_p1');
  });

  it('commits valid edited prefix and invokes onPrefixChange', () => {
    const onPrefixChange = vi.fn();
    renderInProvider(
      <ProfileNode
        id="node1"
        data={{
          prefix: 'agy_p1',
          label: 'agy_p1',
          isRoot: true,
          onPrefixChange,
        }}
        isConnectable={true}
      />
    );

    // Enter edit mode
    fireEvent.click(screen.getByTestId('btn-edit-prefix-node1'));

    const input = screen.getByTestId('input-prefix-node1');
    fireEvent.change(input, { target: { value: 'custom_pool_alpha' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-node1'));

    expect(onPrefixChange).toHaveBeenCalledWith('node1', 'custom_pool_alpha');
    // Exits edit mode
    expect(screen.queryByTestId('input-prefix-node1')).toBeNull();
  });

  it('allows exact empty prefix per Management API', () => {
    const onPrefixChange = vi.fn();
    renderInProvider(
      <ProfileNode
        id="node1"
        data={{
          prefix: 'agy_p1',
          label: 'agy_p1',
          isRoot: false,
          onPrefixChange,
        }}
        isConnectable={true}
      />
    );

    fireEvent.click(screen.getByTestId('btn-edit-prefix-node1'));
    const input = screen.getByTestId('input-prefix-node1');
    fireEvent.change(input, { target: { value: '' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-node1'));

    expect(onPrefixChange).toHaveBeenCalledWith('node1', '');
  });

  it('rejects invalid prefix format and shows validation error without committing', () => {
    const onPrefixChange = vi.fn();
    renderInProvider(
      <ProfileNode
        id="node1"
        data={{
          prefix: 'agy_p1',
          label: 'agy_p1',
          isRoot: true,
          onPrefixChange,
        }}
        isConnectable={true}
      />
    );

    fireEvent.click(screen.getByTestId('btn-edit-prefix-node1'));
    const input = screen.getByTestId('input-prefix-node1');
    fireEvent.change(input, { target: { value: 'invalid prefix with spaces' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-node1'));

    expect(screen.getByTestId('validation-error-node1')).toBeTruthy();
    expect(onPrefixChange).not.toHaveBeenCalled();
    // Remains in edit mode
    expect(screen.getByTestId('input-prefix-node1')).toBeTruthy();
  });

  it('cancels inline editing when Escape or cancel button is pressed', () => {
    const onPrefixChange = vi.fn();
    renderInProvider(
      <ProfileNode
        id="node1"
        data={{
          prefix: 'agy_p1',
          label: 'agy_p1',
          isRoot: true,
          onPrefixChange,
        }}
        isConnectable={true}
      />
    );

    fireEvent.click(screen.getByTestId('btn-edit-prefix-node1'));
    const input = screen.getByTestId('input-prefix-node1');
    fireEvent.change(input, { target: { value: 'cancelled_edit' } });
    fireEvent.click(screen.getByTestId('btn-cancel-prefix-node1'));

    expect(onPrefixChange).not.toHaveBeenCalled();
    expect(screen.queryByTestId('input-prefix-node1')).toBeNull();
    expect(screen.getByText('agy_p1')).toBeTruthy();
  });

  it('explains and prevents renaming parent node while attached children exist', () => {
    const onPrefixChange = vi.fn();
    renderInProvider(
      <ProfileNode
        id="parent1"
        data={{
          prefix: 'agy_p1',
          label: 'agy_p1',
          isRoot: true,
          outgoingCount: 2, // Has 2 attached children!
          onPrefixChange,
        }}
        isConnectable={true}
      />
    );

    // Clicking edit shows explanation immediately
    fireEvent.click(screen.getByTestId('btn-edit-prefix-parent1'));
    expect(screen.getByTestId('validation-error-parent1')).toBeTruthy();
    expect(
      screen.getByText('Cannot rename parent with attached children. Disconnect children first.')
    ).toBeTruthy();

    // Trying to save a changed prefix is rejected
    const input = screen.getByTestId('input-prefix-parent1');
    fireEvent.change(input, { target: { value: 'renamed_p1' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-parent1'));

    expect(onPrefixChange).not.toHaveBeenCalled();
    expect(
      screen.getByText('Cannot rename parent with attached children. Disconnect children first.')
    ).toBeTruthy();
  });

  it('visibly explains ambiguous parent prefix without guessing relation', () => {
    renderInProvider(
      <ProfileNode
        id="child-ambig.json"
        data={{
          prefix: 'agy_p1_1',
          label: 'agy_p1_1',
          isRoot: false,
          ambiguousParent: {
            parentPrefix: 'agy_p1',
            candidateCount: 2,
            candidateNames: ['acc1.json', 'acc2.json'],
          },
        }}
        isConnectable={true}
      />
    );

    // Verifies badge and footer notice visibly explain ambiguity without guessing parent
    expect(screen.getByTestId('badge-ambiguous-child-ambig.json')).toBeTruthy();
    expect(screen.getByText('AMBIGUOUS PARENT')).toBeTruthy();
    expect(screen.getByTestId('ambiguity-notice-child-ambig.json')).toBeTruthy();
    expect(screen.getByText(/Multiple accounts share/)).toBeTruthy();
  });
});
