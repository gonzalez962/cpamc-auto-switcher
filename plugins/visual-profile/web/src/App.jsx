import React from 'react';
import ProfileGraph from './components/ProfileGraph';
import './App.css';

export default function App() {
  return (
    <div className="app-layout">
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
        </div>
      </header>

      <main className="app-main">
        <ProfileGraph />
      </main>
    </div>
  );
}
