import React, { memo, useState, useEffect, useCallback } from 'react';
import { Handle, Position } from '@xyflow/react';

/**
 * Validates prefix format:
 * - Empty string is allowed per API ("exact allowed empty per API")
 * - Non-empty string must contain only alphanumeric characters, underscores, and hyphens
 *
 * @param {string} val
 * @returns {{ valid: boolean, value: string, error?: string }}
 */
export function validateNodePrefix(val) {
  if (val === null || val === undefined) {
    return { valid: true, value: '' };
  }
  const trimmed = String(val).trim();
  if (trimmed === '') {
    return { valid: true, value: '' };
  }
  if (!/^[a-zA-Z0-9_-]+$/.test(trimmed)) {
    return {
      valid: false,
      value: trimmed,
      error: 'Prefix may only contain letters, numbers, underscores, and hyphens.',
    };
  }
  return { valid: true, value: trimmed };
}

/**
 * Custom node component for Profile nodes in the React Flow graph.
 *
 * Features:
 * - Displays node prefix, role (ROOT vs CHILD), and dirty status.
 * - Inline editing with nopan/nodrag classes to avoid dragging canvas or node during text input.
 * - Validates nonempty or exact allowed empty per Management API.
 * - Updates node label and dirty state atomically via data.onPrefixChange callback.
 * - Handles incoming (target) and outgoing (source) connection points.
 */
function ProfileNode({ id, data, isConnectable }) {
  const currentPrefix = data?.prefix !== undefined ? data.prefix : data?.label || id;
  const isRoot = Boolean(data?.isRoot);
  const isDirty = Boolean(data?.isDirty);
  const outgoingCount = data?.outgoingCount ?? 0;
  const fileName = data?.fileName;
  const ambiguousParent = data?.ambiguousParent;

  const [isEditing, setIsEditing] = useState(false);
  const [editValue, setEditValue] = useState(currentPrefix || '');
  const [error, setError] = useState('');

  // Keep edit input synchronized if external prefix changes (e.g. onConnect)
  useEffect(() => {
    setEditValue(currentPrefix || '');
    setError('');
  }, [currentPrefix]);

  const handleStartEdit = useCallback(() => {
    setEditValue(currentPrefix || '');
    if (isRoot && outgoingCount > 0) {
      setError(
        'Cannot rename parent with attached children. Disconnect children first.'
      );
    } else {
      setError('');
    }
    setIsEditing(true);
  }, [currentPrefix, isRoot, outgoingCount]);

  const handleSave = useCallback(() => {
    const result = validateNodePrefix(editValue);
    if (!result.valid) {
      setError(result.error);
      return;
    }

    if (isRoot && outgoingCount > 0 && result.value !== currentPrefix) {
      setError(
        'Cannot rename parent with attached children. Disconnect children first.'
      );
      return;
    }

    if (typeof data?.onPrefixChange === 'function') {
      const res = data.onPrefixChange(id, result.value);
      if (res && res.error) {
        setError(res.error);
        return;
      }
    }

    setError('');
    setIsEditing(false);
  }, [editValue, id, data, isRoot, outgoingCount, currentPrefix]);

  const handleCancel = useCallback(() => {
    setEditValue(currentPrefix || '');
    setError('');
    setIsEditing(false);
  }, [currentPrefix]);

  return (
    <div
      className={`profile-node ${isRoot ? 'profile-node-root' : 'profile-node-child'} ${
        isDirty ? 'profile-node-dirty' : ''
      }`}
      data-testid={`profile-node-${id}`}
    >
      {/* Target handle at the top for incoming connections (children) */}
      <Handle
        type="target"
        position={Position.Top}
        isConnectable={isConnectable}
        className="profile-handle profile-handle-target"
      />

      {/* Node Header */}
      <div className="profile-node-header">
        <div className="profile-node-badges">
          <span className={`profile-badge ${isRoot ? 'badge-root' : 'badge-child'}`}>
            {isRoot ? 'ROOT' : 'CHILD'}
          </span>
          {isDirty && (
            <span className="profile-badge badge-dirty" data-testid={`badge-dirty-${id}`}>
              DIRTY
            </span>
          )}
          {ambiguousParent && (
            <span
              className="profile-badge badge-ambiguous"
              title={`Ambiguous parent: multiple accounts (${ambiguousParent.candidateNames.join(
                ', '
              )}) share prefix "${ambiguousParent.parentPrefix}". Parent relation left disconnected.`}
              data-testid={`badge-ambiguous-${id}`}
            >
              AMBIGUOUS PARENT
            </span>
          )}
        </div>
        <span className="profile-node-id" title={fileName || id}>
          #{fileName || id}
        </span>
      </div>

      {/* Node Body with Inline Editing */}
      <div className="profile-node-body">
        {isEditing ? (
          <div className="profile-node-edit nodrag nopan">
            <input
              type="text"
              className="profile-node-input nodrag nopan"
              value={editValue}
              onChange={(e) => {
                setEditValue(e.target.value);
                setError('');
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  handleSave();
                } else if (e.key === 'Escape') {
                  e.preventDefault();
                  handleCancel();
                }
              }}
              autoFocus
              placeholder="Empty or prefix"
              data-testid={`input-prefix-${id}`}
            />
            <div className="profile-node-edit-actions nodrag nopan">
              <button
                type="button"
                className="btn-node-action btn-save-prefix nodrag nopan"
                onClick={handleSave}
                title="Save prefix (Enter)"
                data-testid={`btn-save-prefix-${id}`}
              >
                ✓
              </button>
              <button
                type="button"
                className="btn-node-action btn-cancel-prefix nodrag nopan"
                onClick={handleCancel}
                title="Cancel (Esc)"
                data-testid={`btn-cancel-prefix-${id}`}
              >
                ✕
              </button>
            </div>
            {error && (
              <div
                className="profile-node-error nodrag nopan"
                data-testid={`validation-error-${id}`}
              >
                {error}
              </div>
            )}
          </div>
        ) : (
          <div className="profile-node-display">
            <div
              className="profile-node-prefix"
              title={currentPrefix || '(empty prefix)'}
              onClick={handleStartEdit}
              data-testid={`profile-node-prefix-${id}`}
            >
              {currentPrefix || <span className="prefix-empty-placeholder">&lt;empty&gt;</span>}
            </div>
            <button
              type="button"
              className="btn-edit-prefix nodrag nopan"
              onClick={handleStartEdit}
              title={
                isRoot && outgoingCount > 0
                  ? 'Cannot rename parent with attached children'
                  : 'Edit prefix'
              }
              data-testid={`btn-edit-prefix-${id}`}
              aria-label="Edit prefix"
            >
              ✏️
            </button>
          </div>
        )}
      </div>

      {/* Root Node Footer with outgoing count */}
      {isRoot && (
        <div className="profile-node-footer">
          <span className="profile-node-subtext">
            {outgoingCount} {outgoingCount === 1 ? 'child' : 'children'}
          </span>
        </div>
      )}

      {/* Visibly explain ambiguous parent relation without guessing */}
      {ambiguousParent && (
        <div
          className="profile-node-footer profile-node-ambiguous-footer"
          data-testid={`ambiguity-notice-${id}`}
        >
          <span
            className="profile-node-subtext text-warning"
            title={`Candidates: ${ambiguousParent.candidateNames.join(', ')}`}
          >
            Multiple accounts share &ldquo;{ambiguousParent.parentPrefix}&rdquo; (disconnected)
          </span>
        </div>
      )}

      {/* Source handle at the bottom for outgoing connections (parents) */}
      <Handle
        type="source"
        position={Position.Bottom}
        isConnectable={isConnectable}
        className="profile-handle profile-handle-source"
      />
    </div>
  );
}

export default memo(ProfileNode);
