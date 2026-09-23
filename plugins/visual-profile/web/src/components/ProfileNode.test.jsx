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

  describe('VP-7 Type Badges, Masked Email & Disconnect Button Interactions', () => {
    it('renders provider/type badge when accountType is present', () => {
      renderInProvider(
        <ProfileNode
          id="node-type"
          data={{
            prefix: 'agy_p1',
            accountType: 'antigravity',
          }}
          isConnectable={true}
        />
      );

      const badge = screen.getByTestId('badge-type-node-type');
      expect(badge).toBeTruthy();
      expect(badge.textContent).toBe('ANTIGRAVITY');
    });

    it('renders masked email safely and never exposes full raw email in DOM attributes', () => {
      renderInProvider(
        <ProfileNode
          id="node-email"
          data={{
            prefix: 'agy_p1',
            maskedEmail: 'ant...unt@domain.org',
          }}
          isConnectable={true}
        />
      );

      const emailEl = screen.getByTestId('profile-node-email-node-email');
      expect(emailEl).toBeTruthy();
      expect(emailEl.textContent).toBe('ant...unt@domain.org');

      // Verify DOM tree does not leak any fake raw email
      const containerHtml = document.body.innerHTML;
      expect(containerHtml).not.toContain('antigravity.superaccount@domain.org');
    });

    it('renders Disconnect button only when node has a parent', () => {
      const { unmount } = renderInProvider(
        <ProfileNode
          id="child-with-parent"
          data={{
            prefix: 'agy_p1_1',
            hasParent: true,
          }}
          isConnectable={true}
        />
      );
      expect(screen.getByTestId('btn-disconnect-child-with-parent')).toBeTruthy();
      unmount();

      // Root / unlinked node without parent
      renderInProvider(
        <ProfileNode
          id="root-no-parent"
          data={{
            prefix: 'agy_p1',
            hasParent: false,
          }}
          isConnectable={true}
        />
      );
      expect(screen.queryByTestId('btn-disconnect-root-no-parent')).toBeNull();
    });

    it('blocks disconnect and shows visible error when child has attached descendants', () => {
      const onDisconnect = vi.fn();
      renderInProvider(
        <ProfileNode
          id="intermediate-child"
          data={{
            prefix: 'agy_p1_1',
            hasParent: true,
            outgoingCount: 2, // has 2 descendants!
            onDisconnect,
          }}
          isConnectable={true}
        />
      );

      const disconnectBtn = screen.getByTestId('btn-disconnect-intermediate-child');
      fireEvent.click(disconnectBtn);

      // Must NOT invoke onDisconnect
      expect(onDisconnect).not.toHaveBeenCalled();

      // Must display visible error
      const errorEl = screen.getByTestId('validation-error-intermediate-child');
      expect(errorEl).toBeTruthy();
      expect(errorEl.textContent).toContain(
        'Cannot disconnect profile with attached descendants. Disconnect descendants first.'
      );
    });

    it('successfully invokes onDisconnect when child has zero descendants', () => {
      const onDisconnect = vi.fn();
      renderInProvider(
        <ProfileNode
          id="leaf-child"
          data={{
            prefix: 'agy_p1_1',
            hasParent: true,
            outgoingCount: 0, // leaf node
            onDisconnect,
          }}
          isConnectable={true}
        />
      );

      const disconnectBtn = screen.getByTestId('btn-disconnect-leaf-child');
      fireEvent.click(disconnectBtn);

      expect(onDisconnect).toHaveBeenCalledWith('leaf-child');
      expect(screen.queryByTestId('validation-error-leaf-child')).toBeNull();
    });

    it('verifies full email is absent from node outerHTML while masked email appears across all generated DOM attributes', () => {
      renderInProvider(
        <ProfileNode
          id="developer.user@openai.com.json"
          data={{
            fileName: 'developer.user@openai.com.json',
            prefix: 'agy_p1',
            maskedEmail: 'dev...ser@openai.com',
            hasParent: true,
            accountType: 'openai',
          }}
          isConnectable={true}
        />
      );

      // Verify node is queryable by its safe masked testid
      const nodeEl = screen.getByTestId('profile-node-dev...ser@openai.com.json');
      expect(nodeEl).toBeTruthy();

      // Check outerHTML for our generated component DOM tree
      const outerHtml = nodeEl.outerHTML;

      // 1. Full raw email local part and full raw email are completely absent from our generated DOM
      expect(outerHtml).not.toContain('developer.user@openai.com');
      expect(outerHtml).not.toContain('developer.user');

      // 2. Safe masked email appears in header, title tooltip, and email subheader
      expect(outerHtml).toContain('dev...ser@openai.com.json');
      expect(outerHtml).toContain('dev...ser@openai.com');
      expect(outerHtml).toContain('data-testid="profile-node-dev...ser@openai.com.json"');
      expect(outerHtml).toContain('data-testid="badge-type-dev...ser@openai.com.json"');
      expect(outerHtml).toContain('data-testid="profile-node-email-dev...ser@openai.com.json"');
      expect(outerHtml).toContain('data-testid="btn-edit-prefix-dev...ser@openai.com.json"');
      expect(outerHtml).toContain('data-testid="btn-disconnect-dev...ser@openai.com.json"');
    });

    it('masks raw email in ambiguous parent badge title and footer notice tooltip', () => {
      renderInProvider(
        <ProfileNode
          id="child-with-ambig"
          data={{
            prefix: 'agy_p1_1',
            ambiguousParent: {
              parentPrefix: 'agy_p1',
              candidateCount: 2,
              candidateNames: [
                'developer.user@openai.com.json',
                'antigravity-user.name@provider.org.json',
              ],
            },
          }}
          isConnectable={true}
        />
      );

      // Ambiguous badge title has masked emails
      const badge = screen.getByTestId('badge-ambiguous-child-with-ambig');
      expect(badge.getAttribute('title')).toContain('dev...ser@openai.com.json');
      expect(badge.getAttribute('title')).toContain('antigravity-use...ame@provider.org.json');

      // Footer notice title has masked emails
      const notice = screen.getByTestId('ambiguity-notice-child-with-ambig');
      const noticeText = notice.querySelector('.profile-node-subtext');
      expect(noticeText.getAttribute('title')).toContain('dev...ser@openai.com.json');
      expect(noticeText.getAttribute('title')).toContain('antigravity-use...ame@provider.org.json');

      // Raw unmasked emails are not in tooltips
      expect(badge.getAttribute('title')).not.toContain('developer.user@openai.com.json');
      expect(noticeText.getAttribute('title')).not.toContain('antigravity-user.name@provider.org.json');
    });

    it('blocks inline editing on intermediate child nodes with outgoingCount > 0 irrespective of isRoot', () => {
      const onPrefixChange = vi.fn();
      renderInProvider(
        <ProfileNode
          id="intermediate-child"
          data={{
            prefix: 'agy_p1_1',
            label: 'agy_p1_1',
            isRoot: false, // Child node!
            outgoingCount: 2, // But has descendants!
            hasParent: true,
            onPrefixChange,
          }}
          isConnectable={true}
        />
      );

      // Edit button has descriptive guard title
      const editBtn = screen.getByTestId('btn-edit-prefix-intermediate-child');
      expect(editBtn.getAttribute('title')).toBe('Cannot rename parent with attached children');

      // Clicking edit immediately reveals the rename guard explanation
      fireEvent.click(editBtn);
      const errorEl = screen.getByTestId('validation-error-intermediate-child');
      expect(errorEl).toBeTruthy();
      expect(errorEl.textContent).toBe(
        'Cannot rename parent with attached children. Disconnect children first.'
      );

      // Attempting to change prefix and save is strictly blocked
      const input = screen.getByTestId('input-prefix-intermediate-child');
      fireEvent.change(input, { target: { value: 'agy_renamed_child' } });
      const saveBtn = screen.getByTestId('btn-save-prefix-intermediate-child');
      fireEvent.click(saveBtn);

      expect(onPrefixChange).not.toHaveBeenCalled();
      expect(
        screen.getByText('Cannot rename parent with attached children. Disconnect children first.')
      ).toBeTruthy();
    });
  });
});
