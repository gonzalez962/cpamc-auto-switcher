import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  deobfuscateCPAMC,
  obfuscateCPAMC,
  getManagementKey,
  assertSafeSameOrigin,
  apiFetch,
  listAuthFiles,
  downloadAuthFilePrefix,
  loadAuthFilesWithPrefixes,
  buildGraphFromAuthFiles,
  sanitizeFileName,
  patchAuthFilePrefix,
  saveAuthFilePrefixes,
} from './managementClient';

describe('Management API Client - Auth & Security Invariants', () => {
  const TEST_HOST = 'cpamc.local:8080';
  const TEST_UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)';
  const TEST_KEY = 'secret-mgmt-key-998877';

  it('correctly round-trips obfuscation and deobfuscation with CPAMC cipher', () => {
    const rawPayload = JSON.stringify({ state: { managementKey: TEST_KEY } });
    const obfuscated = obfuscateCPAMC(rawPayload, TEST_HOST, TEST_UA);

    expect(obfuscated.startsWith('enc::v1::')).toBe(true);

    const deobfuscated = deobfuscateCPAMC(obfuscated, TEST_HOST, TEST_UA);
    expect(deobfuscated).toBe(rawPayload);
    expect(JSON.parse(deobfuscated).state.managementKey).toBe(TEST_KEY);
  });

  it('returns raw text unchanged if not starting with enc::v1::', () => {
    const plain = JSON.stringify({ state: { managementKey: 'plain_key' } });
    expect(deobfuscateCPAMC(plain, TEST_HOST, TEST_UA)).toBe(plain);
    expect(deobfuscateCPAMC('', TEST_HOST, TEST_UA)).toBe('');
    expect(deobfuscateCPAMC(null, TEST_HOST, TEST_UA)).toBe('');
  });

  it('extracts management key from parent storage in iframe mode', () => {
    const rawPayload = JSON.stringify({ state: { managementKey: TEST_KEY } });
    const obfuscated = obfuscateCPAMC(rawPayload, TEST_HOST, TEST_UA);

    const mockParentStorage = {
      getItem: vi.fn((key) => (key === 'cli-proxy-auth' ? obfuscated : null)),
    };

    const key = getManagementKey({
      parentStorage: mockParentStorage,
      host: TEST_HOST,
      userAgent: TEST_UA,
    });

    expect(key).toBe(TEST_KEY);
    expect(mockParentStorage.getItem).toHaveBeenCalledWith('cli-proxy-auth');
  });

  it('falls back to local storage when parent storage is absent', () => {
    const rawPayload = JSON.stringify({ state: { managementKey: 'local-key-456' } });
    const obfuscated = obfuscateCPAMC(rawPayload, TEST_HOST, TEST_UA);

    const mockLocalStorage = {
      getItem: vi.fn((key) => (key === 'cli-proxy-auth' ? obfuscated : null)),
    };

    const key = getManagementKey({
      parentStorage: null,
      localStorage: mockLocalStorage,
      host: TEST_HOST,
      userAgent: TEST_UA,
    });

    expect(key).toBe('local-key-456');
  });

  it('fails closed and returns empty string when storage has no key', () => {
    const mockStorage = { getItem: vi.fn(() => null) };
    const key = getManagementKey({
      parentStorage: mockStorage,
      localStorage: mockStorage,
    });
    expect(key).toBe('');
  });

  it('enforces safe same-origin endpoints and rejects cross-origin URLs', () => {
    // Valid same-origin relative URLs
    expect(() => assertSafeSameOrigin('/v0/management/auth-files')).not.toThrow();
    expect(
      () => assertSafeSameOrigin('/v0/management/auth-files/download?name=acc.json')
    ).not.toThrow();

    // Protocol-relative URLs MUST be rejected
    expect(() => assertSafeSameOrigin('//attacker.com/v0/management')).toThrow(
      /protocol-relative/
    );

    // Cross-origin absolute URLs MUST be rejected
    expect(() => assertSafeSameOrigin('https://evil.org/api')).toThrow(
      /cross-origin/
    );

    // Empty or invalid
    expect(() => assertSafeSameOrigin('')).toThrow(/empty/);
  });

  it('fails closed when apiFetch is called without a valid management key', async () => {
    await expect(
      apiFetch('/v0/management/auth-files', {
        key: '',
        parentStorage: { getItem: () => null },
        localStorage: { getItem: () => null },
      })
    ).rejects.toThrow(/Management key not found/);
  });

  it('case-insensitively strips and ignores malicious caller Authorization headers to ensure key is final', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ files: [] }),
    });

    // 1. Plain object with lowercase 'authorization' and uppercase 'AUTHORIZATION'
    await apiFetch('/v0/management/auth-files', {
      key: 'authoritative-mgmt-key',
      fetchFn: mockFetch,
      headers: {
        authorization: 'Bearer attacker-fake-token',
        AUTHORIZATION: 'Bearer attacker-override',
        'X-Safe-Header': 'preserve-me',
      },
    });

    expect(mockFetch).toHaveBeenCalledTimes(1);
    const [, init1] = mockFetch.mock.calls[0];
    expect(init1.headers.Authorization).toBe('Bearer authoritative-mgmt-key');
    expect(init1.headers.authorization).toBeUndefined();
    expect(init1.headers.AUTHORIZATION).toBeUndefined();
    expect(init1.headers['X-Safe-Header']).toBe('preserve-me');

    // 2. Fetch API Headers instance with lowercase authorization
    const customHeaders = new Headers();
    customHeaders.append('authorization', 'Bearer malicious-token-via-headers-instance');
    customHeaders.append('x-custom-metric', '12345');

    await apiFetch('/v0/management/auth-files', {
      key: 'authoritative-mgmt-key',
      fetchFn: mockFetch,
      headers: customHeaders,
    });

    expect(mockFetch).toHaveBeenCalledTimes(2);
    const [, init2] = mockFetch.mock.calls[1];
    expect(init2.headers.Authorization).toBe('Bearer authoritative-mgmt-key');
    expect(init2.headers.authorization).toBeUndefined();
    expect(init2.headers['x-custom-metric']).toBe('12345');
  });

  it('sanitizes 401 Unauthorized response without leaking secrets', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      text: async () => 'secret response from backend that should not leak',
    });

    await expect(
      apiFetch('/v0/management/auth-files', {
        key: 'invalid-key-xyz',
        fetchFn: mockFetch,
      })
    ).rejects.toThrow('401 Unauthorized: Invalid or expired management key');
  });
});

describe('listAuthFiles - Fail-Closed Physical Name Enforcement', () => {
  it('accepts entries with explicit physical filenames and returns normalized file objects', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        files: [
          { name: 'valid-acc-1.json', id: '0', type: 'antigravity' },
          { name: 'valid-acc-2.json', id: 'acc_id_4', type: 'antigravity' },
        ],
      }),
    });

    const files = await listAuthFiles({
      key: 'test-key',
      fetchFn: mockFetch,
    });

    expect(files.length).toBe(2);
    expect(files[0].name).toBe('valid-acc-1.json');
    expect(files[0].id).toBe('0');
    expect(files[1].name).toBe('valid-acc-2.json');
    expect(files[1].id).toBe('acc_id_4');
  });

  it('fails closed when an entry is missing physical filename instead of silently skipping', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        files: [
          { name: 'valid-acc-1.json', id: '0' },
          { id: '1', type: 'codex' }, // missing physical name!
        ],
      }),
    });

    await expect(
      listAuthFiles({
        key: 'test-key',
        fetchFn: mockFetch,
      })
    ).rejects.toThrow('Invalid auth files listing: entry (id: "1") is missing a physical filename.');
  });

  it('fails closed when listing response is malformed or entry is not an object', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ files: 'not-an-array' }),
    });

    await expect(
      listAuthFiles({
        key: 'test-key',
        fetchFn: mockFetch,
      })
    ).rejects.toThrow('Invalid auth files listing: response missing "files" array.');
  });
});

describe('Auth Files Download & Prefix-Only Extraction', () => {
  it('extracts ONLY prefix and never stores or returns credentials', async () => {
    const sensitiveCredential = {
      prefix: 'agy_p1',
      token: 'sk-ant-api-top-secret-token',
      client_secret: 'super-confidential-secret',
      refresh_token: 'refresh-token-data',
      chatgpt_account_id: 'chatgpt-acc-777',
      private_key: '-----BEGIN PRIVATE KEY-----',
    };

    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => sensitiveCredential,
    });

    const result = await downloadAuthFilePrefix('antigravity account #1.json', {
      key: 'test-key',
      fetchFn: mockFetch,
    });

    // Validates URL-encoding of filename parameter
    expect(mockFetch).toHaveBeenCalledWith(
      '/v0/management/auth-files/download?name=antigravity%20account%20%231.json',
      expect.anything()
    );

    // Validates extracted fields
    expect(result.name).toBe('antigravity account #1.json');
    expect(result.prefix).toBe('agy_p1');

    // CRITICAL: secrets MUST NOT be present
    expect(result.token).toBeUndefined();
    expect(result.client_secret).toBeUndefined();
    expect(result.refresh_token).toBeUndefined();
    expect(result.private_key).toBeUndefined();
    expect(result._raw).toBeUndefined();
  });

  it('returns empty prefix string when credential JSON lacks prefix', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'active', email: 'test@example.com' }),
    });

    const result = await downloadAuthFilePrefix('unlinked.json', {
      key: 'test-key',
      fetchFn: mockFetch,
    });

    expect(result.name).toBe('unlinked.json');
    expect(result.prefix).toBe('');
  });
});

describe('loadAuthFilesWithPrefixes - Fail-Closed Safety Contract', () => {
  it('combines list and download securely when all files succeed', async () => {
    const mockFetch = vi.fn().mockImplementation((url) => {
      if (url === '/v0/management/auth-files') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            files: [{ name: 'f1.json' }, { name: 'f2.json' }],
          }),
        });
      }
      if (url.includes('f1.json')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ prefix: 'agy_p1', secret: 'discard_me' }),
        });
      }
      if (url.includes('f2.json')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ prefix: 'agy_p1_1', token: 'discard_me' }),
        });
      }
      return Promise.reject(new Error('Unknown url'));
    });

    const records = await loadAuthFilesWithPrefixes({
      key: 'test-key',
      fetchFn: mockFetch,
    });

    expect(records).toEqual([
      { name: 'f1.json', prefix: 'agy_p1' },
      { name: 'f2.json', prefix: 'agy_p1_1' },
    ]);
  });

  it('fails closed on 404 download failure without returning partially editable prefix: "" fallback', async () => {
    const mockFetch = vi.fn().mockImplementation((url) => {
      if (url === '/v0/management/auth-files') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            files: [{ name: 'good.json' }, { name: 'missing.json' }],
          }),
        });
      }
      if (url.includes('good.json')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ prefix: 'agy_p1' }),
        });
      }
      if (url.includes('missing.json')) {
        return Promise.resolve({
          ok: false,
          status: 404,
          json: async () => ({ error: 'not_found' }),
        });
      }
      return Promise.reject(new Error('Unknown url'));
    });

    // MUST reject entire load rather than returning prefix: '' for missing.json
    await expect(
      loadAuthFilesWithPrefixes({
        key: 'test-key',
        fetchFn: mockFetch,
      })
    ).rejects.toThrow('Failed to load auth file "missing.json": 404 Not Found');
  });

  it('loads auth files with legitimate duplicate prefixes in CLIProxyAPI pool', async () => {
    const mockFetch = vi.fn().mockImplementation((url) => {
      if (url === '/v0/management/auth-files') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            files: [{ name: 'account-1.json' }, { name: 'account-2.json' }],
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ prefix: 'duplicate_prefix' }),
      });
    });

    const records = await loadAuthFilesWithPrefixes({
      key: 'test-key',
      fetchFn: mockFetch,
    });

    expect(records).toEqual([
      { name: 'account-1.json', prefix: 'duplicate_prefix' },
      { name: 'account-2.json', prefix: 'duplicate_prefix' },
    ]);
  });
});

describe('buildGraphFromAuthFiles & Credential Leak Prevention', () => {
  it('assigns immutable exact file names as node IDs and links parent/child edges', () => {
    const authFiles = [
      { name: 'auth-root.json', prefix: 'agy_p1' },
      { name: 'auth-child-1.json', prefix: 'agy_p1_1' },
      { name: 'auth-child-2.json', prefix: 'agy_p1_2' },
      { name: 'auth-unlinked.json', prefix: '' },
    ];

    const { nodes, edges } = buildGraphFromAuthFiles(authFiles);

    expect(nodes.length).toBe(4);
    // Node IDs MUST be exact file names
    expect(nodes[0].id).toBe('auth-root.json');
    expect(nodes[1].id).toBe('auth-child-1.json');
    expect(nodes[2].id).toBe('auth-child-2.json');
    expect(nodes[3].id).toBe('auth-unlinked.json');

    // Data verification
    expect(nodes[0].data.fileName).toBe('auth-root.json');
    expect(nodes[0].data.prefix).toBe('agy_p1');
    expect(nodes[0].data.initialPrefix).toBe('agy_p1');
    expect(nodes[0].data.isDirty).toBe(false);
    expect(nodes[0].data.isSynthetic).toBe(false);
    expect(nodes[0].data.isRoot).toBe(true);

    expect(nodes[1].data.isRoot).toBe(false);
    expect(nodes[2].data.isRoot).toBe(false);

    // Edges verification
    expect(edges.length).toBe(2);
    expect(edges[0].source).toBe('auth-root.json');
    expect(edges[0].target).toBe('auth-child-1.json');
    expect(edges[1].source).toBe('auth-root.json');
    expect(edges[1].target).toBe('auth-child-2.json');
  });

  it('builds graph with duplicate prefixes and leaves ambiguous nodes disconnected', () => {
    const filesWithDuplicateParents = [
      { name: 'parent-a.json', prefix: 'agy_p1' },
      { name: 'parent-b.json', prefix: 'agy_p1' },
      { name: 'child-ambiguous.json', prefix: 'agy_p1_1' },
      { name: 'parent-solo.json', prefix: 'agy_p2' },
      { name: 'child-solo.json', prefix: 'agy_p2_1' },
    ];

    const { nodes, edges } = buildGraphFromAuthFiles(filesWithDuplicateParents);

    expect(nodes.length).toBe(5);
    // Graph inference must NOT invent parent edge when duplicate parent prefix (leave ambiguous nodes disconnected)
    // Only child-solo.json has an unambiguous parent (parent-solo.json)
    expect(edges.length).toBe(1);
    expect(edges[0].source).toBe('parent-solo.json');
    expect(edges[0].target).toBe('child-solo.json');

    // child-ambiguous.json remains disconnected (no edge invented)
    expect(edges.some((e) => e.target === 'child-ambiguous.json')).toBe(false);
  });
});

describe('sanitizeFileName Helper', () => {
  it('sanitizes control characters and preserves valid file names', () => {
    expect(sanitizeFileName('normal-account.json')).toBe('normal-account.json');
    expect(sanitizeFileName('antigravity account #1.json')).toBe('antigravity account #1.json');
    expect(sanitizeFileName('bad\x00file\x1fname.json')).toBe('badfilename.json');
    expect(sanitizeFileName('')).toBe('unknown');
    expect(sanitizeFileName(null)).toBe('unknown');
  });
});

describe('patchAuthFilePrefix - Management PATCH /v0/management/auth-files/fields', () => {
  it('sends PATCH request with exact physical filename and prefix', async () => {
    let capturedUrl = null;
    let capturedOptions = null;

    const mockFetch = vi.fn().mockImplementation((url, opts) => {
      capturedUrl = url;
      capturedOptions = opts;
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok' }),
      });
    });

    const res = await patchAuthFilePrefix('account-1.json', 'agy_p1', {
      key: 'secret-mgmt-key-123',
      fetchFn: mockFetch,
    });

    expect(capturedUrl).toBe('/v0/management/auth-files/fields');
    expect(capturedOptions.method).toBe('PATCH');
    expect(capturedOptions.headers['Content-Type']).toBe('application/json');
    expect(capturedOptions.headers['Authorization']).toBe('Bearer secret-mgmt-key-123');

    const body = JSON.parse(capturedOptions.body);
    expect(body).toEqual({
      name: 'account-1.json',
      prefix: 'agy_p1',
    });

    expect(res).toEqual({
      name: 'account-1.json',
      prefix: 'agy_p1',
      status: 200,
    });
  });

  it('allows empty string prefix explicitly per API contract', async () => {
    let capturedBody = null;

    const mockFetch = vi.fn().mockImplementation((url, opts) => {
      capturedBody = JSON.parse(opts.body);
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok' }),
      });
    });

    const res = await patchAuthFilePrefix('cleared.json', '', {
      key: 'test-key',
      fetchFn: mockFetch,
    });

    expect(capturedBody).toEqual({
      name: 'cleared.json',
      prefix: '',
    });
    expect(res.prefix).toBe('');
  });

  it('fails closed when filename is invalid or empty', async () => {
    await expect(patchAuthFilePrefix('', 'agy_p1', { key: 'k' })).rejects.toThrow(
      'Invalid auth file name: filename must be a non-empty string'
    );
    await expect(patchAuthFilePrefix(null, 'agy_p1', { key: 'k' })).rejects.toThrow(
      'Invalid auth file name: filename must be a non-empty string'
    );
  });

  it('handles 401 Unauthorized with sanitized error without leaking bearer key', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({ error: 'unauthorized' }),
    });

    const promise = patchAuthFilePrefix('acc.json', 'p1', {
      key: 'super-sensitive-secret-token',
      fetchFn: mockFetch,
    });

    await expect(promise).rejects.toThrow(
      '401 Unauthorized: Invalid or expired management key for "acc.json"'
    );
    // Crucial check: verify that sensitive token does not leak in error message
    await expect(promise).rejects.not.toThrow('super-sensitive-secret-token');
  });

  it('handles 404 Not Found with sanitized error', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({ error: 'not found' }),
    });

    await expect(
      patchAuthFilePrefix('missing.json', 'p1', {
        key: 'test-key',
        fetchFn: mockFetch,
      })
    ).rejects.toThrow('404 Not Found: Auth file "missing.json" not found');
  });

  it('handles 409 Conflict with sanitized error', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'conflict' }),
    });

    await expect(
      patchAuthFilePrefix('conflict.json', 'p1', {
        key: 'test-key',
        fetchFn: mockFetch,
      })
    ).rejects.toThrow('409 Conflict: Resource conflict for "conflict.json"');
  });

  it('strictly disallows options from overriding method, body, or Content-Type for patch', async () => {
    let capturedOptions = null;
    const mockFetch = vi.fn().mockImplementation((url, opts) => {
      capturedOptions = opts;
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok' }),
      });
    });

    // Malicious caller attempts to override HTTP method, body, and Content-Type
    await patchAuthFilePrefix('legit-account.json', 'valid_prefix', {
      key: 'safe-mgmt-key',
      fetchFn: mockFetch,
      method: 'DELETE',
      body: '{"malicious_overwrite": true}',
      headers: {
        'content-type': 'text/plain',
        'Content-Type': 'application/xml',
        'X-Allowed-Audit': 'audit-999',
      },
    });

    expect(mockFetch).toHaveBeenCalledTimes(1);
    // Method MUST be PATCH
    expect(capturedOptions.method).toBe('PATCH');
    // Body MUST be JSON string of legitimate record
    expect(JSON.parse(capturedOptions.body)).toEqual({
      name: 'legit-account.json',
      prefix: 'valid_prefix',
    });
    // Content-Type MUST be strictly application/json
    expect(capturedOptions.headers['Content-Type']).toBe('application/json');
    expect(capturedOptions.headers['content-type']).toBeUndefined();
    // Custom non-conflicting headers MUST be preserved
    expect(capturedOptions.headers['X-Allowed-Audit']).toBe('audit-999');
    // Authorization must be authoritative key
    expect(capturedOptions.headers.Authorization).toBe('Bearer safe-mgmt-key');
  });
});

describe('saveAuthFilePrefixes - Multi-File Staging and Non-Atomic Partial Failure Handling', () => {
  it('saves multiple files and returns successful records', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'ok' }),
    });

    const result = await saveAuthFilePrefixes(
      [
        { name: 'acc1.json', prefix: 'agy_p1' },
        { name: 'acc2.json', prefix: 'agy_p1_1' },
      ],
      { key: 'test-key', fetchFn: mockFetch }
    );

    expect(result.successful.length).toBe(2);
    expect(result.failed.length).toBe(0);
    expect(result.successful).toEqual([
      { name: 'acc1.json', prefix: 'agy_p1' },
      { name: 'acc2.json', prefix: 'agy_p1_1' },
    ]);
  });

  it('filters out synthetic nodes and never sends them to the Management API', async () => {
    const calls = [];
    const mockFetch = vi.fn().mockImplementation((url, opts) => {
      calls.push(JSON.parse(opts.body));
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok' }),
      });
    });

    const result = await saveAuthFilePrefixes(
      [
        { name: 'real-1.json', prefix: 'agy_p1', isSynthetic: false },
        { name: 'synthetic-node-1', prefix: 'syn_p1', isSynthetic: true },
        { name: 'real-2.json', prefix: 'agy_p1_1', isSynthetic: false },
      ],
      { key: 'test-key', fetchFn: mockFetch }
    );

    expect(calls.length).toBe(2);
    expect(calls.some((c) => c.name === 'synthetic-node-1')).toBe(false);
    expect(result.successful.length).toBe(2);
    expect(result.successful.map((s) => s.name)).toEqual(['real-1.json', 'real-2.json']);
  });

  it('permits saving duplicate prefixes for legitimate account rotation pools in CLIProxyAPI', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ok: true }),
    });

    const result = await saveAuthFilePrefixes(
      [
        { name: 'acc1.json', prefix: 'duplicate_pool' },
        { name: 'acc2.json', prefix: 'duplicate_pool' },
      ],
      { key: 'test-key', fetchFn: mockFetch }
    );

    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(result.successful.length).toBe(2);
    expect(result.failed.length).toBe(0);
  });

  it('handles multi-file partial failure without claiming atomicity (Promise.allSettled)', async () => {
    const mockFetch = vi.fn().mockImplementation((url, opts) => {
      const body = JSON.parse(opts.body);
      if (body.name === 'success.json') {
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
          json: async () => ({ error: 'duplicate prefix in backend' }),
        });
      }
      return Promise.reject(new Error('unknown'));
    });

    const result = await saveAuthFilePrefixes(
      [
        { name: 'success.json', prefix: 'agy_new' },
        { name: 'conflict.json', prefix: 'agy_conflict' },
      ],
      { key: 'test-key', fetchFn: mockFetch }
    );

    expect(result.successful).toEqual([{ name: 'success.json', prefix: 'agy_new' }]);
    expect(result.failed.length).toBe(1);
    expect(result.failed[0].name).toBe('conflict.json');
    expect(result.failed[0].error).toContain('409 Conflict');
  });
});
