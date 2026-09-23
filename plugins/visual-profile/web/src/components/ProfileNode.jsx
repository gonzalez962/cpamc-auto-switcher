import React, { memo } from 'react';
import { Handle, Position } from '@xyflow/react';

/**
 * Custom node component for Profile nodes in the React Flow graph.
 * Displays node prefix, role (Root vs Child), and connection handles.
 */
function ProfileNode({ id, data, isConnectable }) {
  const prefix = data?.prefix || data?.label || id;
  const isRoot = Boolean(data?.isRoot);
  const outgoingCount = data?.outgoingCount ?? 0;

  return (
    <div
      className={`profile-node ${isRoot ? 'profile-node-root' : 'profile-node-child'}`}
      data-testid={`profile-node-${id}`}
    >
      {/* Target handle at the top for incoming connections (children) */}
      <Handle
        type="target"
        position={Position.Top}
        isConnectable={isConnectable}
        className="profile-handle profile-handle-target"
      />

      <div className="profile-node-header">
        <span className={`profile-badge ${isRoot ? 'badge-root' : 'badge-child'}`}>
          {isRoot ? 'ROOT' : 'CHILD'}
        </span>
        <span className="profile-node-id">#{id}</span>
      </div>

      <div className="profile-node-body">
        <div className="profile-node-prefix" title={prefix}>
          {prefix}
        </div>
      </div>

      {isRoot && (
        <div className="profile-node-footer">
          <span className="profile-node-subtext">
            {outgoingCount} {outgoingCount === 1 ? 'child' : 'children'}
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
