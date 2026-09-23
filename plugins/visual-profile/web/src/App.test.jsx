import React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import App from './App';
import * as managementClient from './api/managementClient';

// Mock resize observer for JSDOM
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

describe('App Host Component Integration', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders unauthenticated warning when management key is missing', async () => {
    vi.spyOn(managementClient, 'getManagementKey').mockReturnValue('');

    render(<App />);

    expect(await screen.findByTestId('status-unauthenticated')).toBeTruthy();
    expect(screen.getByText('Authentication Required')).toBeTruthy();
    expect(screen.getByTestId('btn-retry-auth')).toBeTruthy();
    expect(screen.getByTestId('btn-load-demo')).toBeTruthy();
  });

  it('switches to offline demo graph when user clicks View Offline Demo', async () => {
    vi.spyOn(managementClient, 'getManagementKey').mockReturnValue('');

    render(<App />);

    const demoBtn = await screen.findByTestId('btn-load-demo');
    fireEvent.click(demoBtn);

    // In demo mode, allowSynthetic is true, so btn-add-root is available
    expect(await screen.findByTestId('btn-add-root')).toBeTruthy();
    expect(screen.getByText('Editor Only — Local Graph')).toBeTruthy();
  });

  it('loads real auth files into ProfileGraph when management key is present', async () => {
    vi.spyOn(managementClient, 'getManagementKey').mockReturnValue('valid-key-123');
    vi.spyOn(managementClient, 'loadAuthFilesWithPrefixes').mockResolvedValue([
      { name: 'antigravity-1.json', prefix: 'agy_p1' },
      { name: 'antigravity-2.json', prefix: 'agy_p1_1' },
    ]);

    render(<App />);

    // Wait for nodes to load into graph
    expect(await screen.findByText('agy_p1')).toBeTruthy();
    expect(screen.getByText('agy_p1_1')).toBeTruthy();

    // Verify production mode avoids arbitrary unbound synthetic nodes
    expect(screen.getByTestId('badge-live-auth')).toBeTruthy();
    expect(screen.queryByTestId('btn-add-root')).toBeNull();
    expect(screen.queryByTestId('btn-add-child')).toBeNull();

    // Header reload button is available
    expect(screen.getByTestId('btn-reload-profiles')).toBeTruthy();
  });

  it('fails closed on 404 download failure and displays sanitized error without rendering editable graph', async () => {
    vi.spyOn(managementClient, 'getManagementKey').mockReturnValue('valid-key-123');
    vi.spyOn(managementClient, 'loadAuthFilesWithPrefixes').mockRejectedValue(
      new Error('Failed to load auth file "corrupted-account.json": 404 Not Found')
    );

    render(<App />);

    expect(await screen.findByTestId('status-error')).toBeTruthy();
    expect(screen.getByText('Failed to Load Profiles')).toBeTruthy();
    expect(
      screen.getByText('Failed to load auth file "corrupted-account.json": 404 Not Found')
    ).toBeTruthy();

    // MUST NOT render ProfileGraph or any editable node
    expect(screen.queryByTestId('rf-wrapper')).toBeNull();
    expect(screen.queryByTestId('btn-add-root')).toBeNull();
    expect(screen.getByTestId('btn-retry-error')).toBeTruthy();
  });

  it('fails closed on 401 unauthorized download failure with sanitized status', async () => {
    vi.spyOn(managementClient, 'getManagementKey').mockReturnValue('valid-key-123');
    vi.spyOn(managementClient, 'loadAuthFilesWithPrefixes').mockRejectedValue(
      new Error('Failed to load auth file "account.json": 401 Unauthorized')
    );

    render(<App />);

    expect(await screen.findByTestId('status-error')).toBeTruthy();
    expect(screen.getByText('Failed to Load Profiles')).toBeTruthy();
    expect(
      screen.getByText('Failed to load auth file "account.json": 401 Unauthorized')
    ).toBeTruthy();
    expect(screen.queryByTestId('rf-wrapper')).toBeNull();
  });

  it('prompts confirmation when reloading with dirty edits and aborts if cancelled', async () => {
    vi.spyOn(managementClient, 'getManagementKey').mockReturnValue('valid-key-123');
    const loadSpy = vi.spyOn(managementClient, 'loadAuthFilesWithPrefixes').mockResolvedValue([
      { name: 'acc1.json', prefix: 'agy_p1' },
    ]);
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);

    render(<App />);

    expect(await screen.findByText('agy_p1')).toBeTruthy();
    expect(loadSpy).toHaveBeenCalledTimes(1);

    // Make node dirty via inline edit
    fireEvent.click(screen.getByTestId('btn-edit-prefix-acc1.json'));
    const input = screen.getByTestId('input-prefix-acc1.json');
    fireEvent.change(input, { target: { value: 'custom_dirty_val' } });
    fireEvent.click(screen.getByTestId('btn-save-prefix-acc1.json'));
    expect(screen.getByText('custom_dirty_val')).toBeTruthy();

    // Click Reload button: confirm prompt appears and returns false
    const reloadBtn = screen.getByTestId('btn-reload-profiles');
    fireEvent.click(reloadBtn);

    expect(confirmSpy).toHaveBeenCalled();
    // loadAuthFilesWithPrefixes must NOT be called again
    expect(loadSpy).toHaveBeenCalledTimes(1);
    expect(screen.getByText('custom_dirty_val')).toBeTruthy();

    // Now confirm reload
    confirmSpy.mockReturnValue(true);
    await act(async () => {
      fireEvent.click(reloadBtn);
    });

    expect(loadSpy).toHaveBeenCalledTimes(2);
  });
});
