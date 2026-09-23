import React, { useState, useEffect, useCallback, useRef } from 'react';
import ProfileGraph from './components/ProfileGraph';
import {
  getManagementKey,
  loadAuthFilesWithPrefixes,
  buildGraphFromAuthFiles,
} from './api/managementClient';
import './App.css';

export default function App() {
  const [status, setStatus] = useState('loading'); // 'loading' | 'unauthenticated' | 'ready' | 'error' | 'demo'
  const [errorMessage, setErrorMessage] = useState('');
  const [graphData, setGraphData] = useState({ nodes: [], edges: [] });
  const [datasetVersion, setDatasetVersion] = useState(0);
  const graphRef = useRef(null);

  const loadProfiles = useCallback(async () => {
    const key = getManagementKey();
    if (!key) {
      setStatus('unauthenticated');
      return;
    }

    setStatus('loading');
    setErrorMessage('');

    try {
      const authFiles = await loadAuthFilesWithPrefixes({ key });
      const { nodes, edges } = buildGraphFromAuthFiles(authFiles);
      setGraphData({ nodes, edges });
      setDatasetVersion((v) => v + 1);
      setStatus('ready');
    } catch (err) {
      // Never expose raw secrets or token material in error state
      setErrorMessage(
        err.message || 'Failed to load authentication profiles from Management API'
      );
      setStatus('error');
    }
  }, []);

  useEffect(() => {
    loadProfiles();
  }, [loadProfiles]);

  const handleUseDemo = useCallback(() => {
    setStatus('demo');
  }, []);

  /**
   * Reloads auth files with confirmation if graph contains unsaved dirty edits.
   * State is retrieved imperatively from the Graph component ref, avoiding React render lag.
   */
  const handleReload = useCallback(() => {
    if (graphRef.current) {
      if (typeof graphRef.current.isSaving === 'function' && graphRef.current.isSaving()) {
        return;
      }
      const currentNodes =
        typeof graphRef.current.getNodes === 'function'
          ? graphRef.current.getNodes()
          : [];
      const hasDirty = currentNodes.some((n) => Boolean(n?.data?.isDirty));
      if (hasDirty) {
        const confirmed =
          typeof window !== 'undefined' && typeof window.confirm === 'function'
            ? window.confirm(
                'You have unsaved prefix modifications. Reloading will discard all changes. Continue?'
              )
            : true;
        if (!confirmed) {
          return;
        }
      }
    }
    loadProfiles();
  }, [loadProfiles]);

  return (
    <div className="app-layout">
      {/* Header */}
      <header className="app-header">
        <div className="header-brand">
          <div className="brand-icon">VP</div>
          <div>
            <h1 className="brand-title">Visual Profile Manager</h1>
            <p className="brand-subtitle">
              Interactive prefix topology graph for parent/child account routing
            </p>
          </div>
        </div>
        <div className="header-meta">
          <span className="badge-status">C-ABI v1</span>
          <span className="badge-route">/profiles</span>
          {(status === 'ready' || status === 'demo') && (
            <button
              type="button"
              className="btn btn-outline"
              onClick={handleReload}
              data-testid="btn-reload-profiles"
              title="Reload auth files from Management API"
            >
              🔄 Reload
            </button>
          )}
        </div>
      </header>

      {/* Main Content Area */}
      <main className="app-main">
        {status === 'loading' && (
          <div className="state-container" data-testid="status-loading">
            <div style={{ textAlign: 'center' }}>
              <div className="loading-spinner" />
              <p style={{ marginTop: 12, color: 'var(--text-secondary)' }}>
                Loading profiles from Management API...
              </p>
            </div>
          </div>
        )}

        {status === 'unauthenticated' && (
          <div className="state-container" data-testid="status-unauthenticated">
            <div className="auth-warning-box">
              <div className="auth-warning-head">
                <span className="auth-warning-icon">🔐</span>
                <div>
                  <h3 className="auth-warning-title">Authentication Required</h3>
                </div>
              </div>
              <p className="auth-warning-desc">
                The visual profile plugin could not retrieve the management key
                (<code>managementKey</code>) from browser storage. In <strong>CPAMC</strong>,
                the key is only accessible inside iframe plugins when logged in with{' '}
                <strong>"Remember password"</strong> enabled.
              </p>
              <div className="auth-warning-steps">
                <strong>How to resolve:</strong>
                <ol>
                  <li>Log in to the main CPAMC console.</li>
                  <li>
                    Ensure the <strong>"Remember password"</strong> checkbox is checked.
                  </li>
                  <li>Reload this panel or click "Retry Authentication" below.</li>
                </ol>
              </div>
              <div className="auth-warning-actions">
                <button
                  type="button"
                  className="btn btn-outline"
                  onClick={handleUseDemo}
                  data-testid="btn-load-demo"
                >
                  View Offline Demo
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={loadProfiles}
                  data-testid="btn-retry-auth"
                >
                  Retry Authentication
                </button>
              </div>
            </div>
          </div>
        )}

        {status === 'error' && (
          <div className="state-container" data-testid="status-error">
            <div className="error-card">
              <h3 style={{ color: '#f85149', marginBottom: 10 }}>Failed to Load Profiles</h3>
              <p style={{ color: 'var(--text-secondary)', marginBottom: 16 }}>
                {errorMessage}
              </p>
              <div style={{ display: 'flex', gap: 10, justifyContent: 'center' }}>
                <button
                  type="button"
                  className="btn btn-outline"
                  onClick={handleUseDemo}
                  data-testid="btn-load-demo-error"
                >
                  View Offline Demo
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={loadProfiles}
                  data-testid="btn-retry-error"
                >
                  Retry
                </button>
              </div>
            </div>
          </div>
        )}

        {status === 'ready' && (
          <ProfileGraph
            ref={graphRef}
            key={`live-graph-${datasetVersion}`}
            initialNodes={graphData.nodes}
            initialEdges={graphData.edges}
            allowSynthetic={false}
          />
        )}

        {status === 'demo' && (
          <ProfileGraph ref={graphRef} key="demo-graph" allowSynthetic={true} />
        )}
      </main>
    </div>
  );
}
