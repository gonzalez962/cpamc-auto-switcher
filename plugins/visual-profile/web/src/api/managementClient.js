/**
 * Web Management API Client for CLIProxyAPI Visual Profile Plugin.
 *
 * Implements:
 * - CPAMC parent/local storage key extraction with XOR deobfuscation (`enc::v1::`).
 * - Fail-closed authentication guard.
 * - Same-origin endpoint enforcement (disallows cross-origin or unsafe URLs).
 * - GET /v0/management/auth-files list retrieval (accepts only explicit physical name, rejects missing name).
 * - GET /v0/management/auth-files/download?name= URL-encoded prefix download.
 * - Fail-closed loadAuthFilesWithPrefixes: any download failure (404/401) aborts entire load with sanitized status.
 * - Prefix-only extraction: credentials/secrets are NEVER stored, returned, or logged.
 * - Graph construction with immutable exact auth-file name IDs, strictly avoiding credential keys in state.
 */

const SECRET_SALT = 'cli-proxy-api-webui::secure-storage';
const ENC_PREFIX = 'enc::v1::';

/**
 * Sanitizes a file name for safe display in error messages without leaking control characters or tokens.
 *
 * @param {string} name
 * @returns {string} Sanitized filename
 */
export function sanitizeFileName(name) {
  if (!name || typeof name !== 'string') return 'unknown';
  // Strip control chars and limit length to avoid log pollution
  const clean = name.replace(/[\x00-\x1f\x7f-\x9f]/g, '').trim();
  if (clean.length > 64) {
    return clean.slice(0, 60) + '...';
  }
  return clean || 'unknown';
}

/**
 * Deobfuscates CPAMC storage payload using host and userAgent XOR cipher.
 *
 * @param {string} raw - The stored string value
 * @param {string} [host] - Hostname override for testing
 * @param {string} [ua] - User agent override for testing
 * @returns {string} Deobfuscated string, or raw if not obfuscated or on error
 */
export function deobfuscateCPAMC(raw, host, ua) {
  if (!raw || typeof raw !== 'string') return '';
  if (!raw.startsWith(ENC_PREFIX)) return raw;

  try {
    const effectiveHost =
      host ||
      (typeof window !== 'undefined' && window.location && window.location.host) ||
      'localhost';
    const effectiveUa =
      ua ||
      (typeof navigator !== 'undefined' && navigator.userAgent) ||
      '';

    const keyStr = `${SECRET_SALT}|${effectiveHost}|${effectiveUa}`;
    const encoder = new TextEncoder();
    const keyBytes = encoder.encode(keyStr);

    const b64 = raw.slice(ENC_PREFIX.length);
    const binary = atob(b64);
    const dataBytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      dataBytes[i] = binary.charCodeAt(i);
    }

    const decrypted = new Uint8Array(dataBytes.length);
    for (let i = 0; i < dataBytes.length; i++) {
      decrypted[i] = dataBytes[i] ^ keyBytes[i % keyBytes.length];
    }

    return new TextDecoder().decode(decrypted);
  } catch {
    // Fail-safe: return raw on decode error without throwing or leaking
    return raw;
  }
}

/**
 * Obfuscates plain text matching CPAMC XOR cipher.
 * Useful for tests and symmetrical verification.
 *
 * @param {string} plainText
 * @param {string} [host]
 * @param {string} [ua]
 * @returns {string} Obfuscated payload with 'enc::v1::' prefix
 */
export function obfuscateCPAMC(plainText, host, ua) {
  if (typeof plainText !== 'string') return '';
  const effectiveHost =
    host ||
    (typeof window !== 'undefined' && window.location && window.location.host) ||
    'localhost';
  const effectiveUa =
    ua ||
    (typeof navigator !== 'undefined' && navigator.userAgent) ||
    '';

  const keyStr = `${SECRET_SALT}|${effectiveHost}|${effectiveUa}`;
  const encoder = new TextEncoder();
  const keyBytes = encoder.encode(keyStr);
  const dataBytes = encoder.encode(plainText);

  const encrypted = new Uint8Array(dataBytes.length);
  for (let i = 0; i < dataBytes.length; i++) {
    encrypted[i] = dataBytes[i] ^ keyBytes[i % keyBytes.length];
  }

  let binary = '';
  for (let i = 0; i < encrypted.length; i++) {
    binary += String.fromCharCode(encrypted[i]);
  }

  return ENC_PREFIX + btoa(binary);
}

/**
 * Retrieves the management key from CPAMC parent storage (iframe) or local storage.
 * Fails closed and returns empty string if key is not found.
 *
 * @param {Object} [options]
 * @param {string} [options.injectedKey] - Directly injected key for test override
 * @param {Storage} [options.parentStorage] - Storage override for parent window
 * @param {Storage} [options.localStorage] - Storage override for local window
 * @param {string} [options.host] - Host override
 * @param {string} [options.userAgent] - User agent override
 * @returns {string} Management key or empty string
 */
export function getManagementKey(options = {}) {
  if (options.injectedKey) {
    return options.injectedKey.trim();
  }

  const effectiveHost = options.host;
  const effectiveUa = options.userAgent;

  // 1. Try parent localStorage (iframe scenario inside CPAMC)
  try {
    const parentStorage =
      options.parentStorage ||
      (typeof window !== 'undefined' && window.parent && window.parent !== window
        ? window.parent.localStorage
        : null);

    if (parentStorage) {
      const raw = parentStorage.getItem('cli-proxy-auth');
      if (raw) {
        const decrypted = deobfuscateCPAMC(raw, effectiveHost, effectiveUa);
        const parsed = JSON.parse(decrypted);
        if (parsed?.state?.managementKey) {
          return String(parsed.state.managementKey).trim();
        }
      }
    }
  } catch {
    // Cross-origin iframe security or parsing error: fall through
  }

  // 2. Try window.localStorage (direct host scenario)
  try {
    const localStorage =
      options.localStorage ||
      (typeof window !== 'undefined' ? window.localStorage : null);

    if (localStorage) {
      const raw = localStorage.getItem('cli-proxy-auth');
      if (raw) {
        const decrypted = deobfuscateCPAMC(raw, effectiveHost, effectiveUa);
        const parsed = JSON.parse(decrypted);
        if (parsed?.state?.managementKey) {
          return String(parsed.state.managementKey).trim();
        }
      }
    }
  } catch {
    // Parsing error: fall through
  }

  // Fail closed
  return '';
}

/**
 * Validates that an endpoint URL is strictly same-origin.
 * Rejects cross-origin, protocol-relative, and invalid URLs.
 *
 * @param {string} endpoint
 * @throws {Error} If endpoint violates same-origin policy
 */
export function assertSafeSameOrigin(endpoint) {
  if (!endpoint || typeof endpoint !== 'string') {
    throw new Error('Invalid endpoint: empty URL');
  }

  const trimmed = endpoint.trim();

  // Relative path starting with / is safe same-origin, unless protocol-relative (//)
  if (trimmed.startsWith('/')) {
    if (trimmed.startsWith('//')) {
      throw new Error('Unsafe origin rejected: protocol-relative URLs not allowed');
    }
    return;
  }

  // Absolute URL: origin MUST match current window.location.origin
  try {
    const baseOrigin =
      typeof window !== 'undefined' && window.location && window.location.origin
        ? window.location.origin
        : 'http://localhost';
    const parsed = new URL(trimmed, baseOrigin);
    const expected = new URL(baseOrigin);

    if (parsed.origin !== expected.origin) {
      throw new Error(
        `Unsafe origin rejected: cross-origin requests to ${parsed.origin} not allowed`
      );
    }
  } catch (err) {
    if (err.message && err.message.includes('Unsafe origin rejected')) {
      throw err;
    }
    throw new Error('Invalid endpoint URL');
  }
}

/**
 * Executes an authenticated API fetch against the Management API.
 * Fails closed if management key is missing.
 * Enforces same-origin policy and sanitizes error responses.
 *
 * @param {string} endpoint - API path or same-origin URL
 * @param {Object} [options]
 * @returns {Promise<Response>}
 */
export async function apiFetch(endpoint, options = {}) {
  assertSafeSameOrigin(endpoint);

  const key = options.key !== undefined ? options.key : getManagementKey(options);
  if (!key) {
    throw new Error(
      'Management key not found. Please log in to CPAMC with "Remember password" enabled.'
    );
  }

  const fetchImpl = options.fetchFn || (typeof fetch !== 'undefined' ? fetch : null);
  if (!fetchImpl) {
    throw new Error('Fetch API is not available in current environment');
  }

  // Safely merge custom headers:
  // SECURITY INVARIANT:
  // Remove/ignore caller Authorization headers case-insensitively (e.g. 'authorization', 'AUTHORIZATION')
  // and ensure the authoritative Bearer management key is final and cannot be overwritten.
  const mergedHeaders = {
    Accept: 'application/json',
  };

  if (options.headers) {
    if (typeof options.headers.forEach === 'function') {
      options.headers.forEach((value, name) => {
        if (typeof name === 'string' && name.toLowerCase() !== 'authorization') {
          mergedHeaders[name] = value;
        }
      });
    } else if (typeof options.headers === 'object') {
      for (const [name, value] of Object.entries(options.headers)) {
        if (typeof name === 'string' && name.toLowerCase() !== 'authorization') {
          mergedHeaders[name] = value;
        }
      }
    }
  }

  // Case-insensitively strip any lingering authorization variants
  for (const headerKey of Object.keys(mergedHeaders)) {
    if (headerKey.toLowerCase() === 'authorization') {
      delete mergedHeaders[headerKey];
    }
  }

  // Final authoritative Authorization header
  mergedHeaders['Authorization'] = `Bearer ${key}`;

  const { headers: _ignoredHeaders, ...restOptions } = options;

  let res;
  try {
    res = await fetchImpl(endpoint, {
      ...restOptions,
      headers: mergedHeaders,
    });
  } catch {
    // Sanitize network errors so no tokens or sensitive connection details leak
    throw new Error('Management API network request failed');
  }

  if (!res.ok) {
    if (res.status === 401) {
      throw new Error('401 Unauthorized: Invalid or expired management key');
    }
    if (res.status === 403) {
      throw new Error('403 Forbidden: Management key does not have required permissions');
    }
    if (res.status === 404) {
      throw new Error('404 Not Found: Resource not found');
    }
    if (res.status === 409) {
      throw new Error('409 Conflict: Resource conflict detected on server');
    }
    throw new Error(`Management API request failed with status ${res.status}`);
  }

  return res;
}

/**
 * Retrieves the list of configured auth files via GET /v0/management/auth-files.
 *
 * SAFETY INVARIANT:
 * Fails closed on malformed listing entries. Strictly requires an explicit, non-empty
 * physical `name` string for every listed item. Never falls back to `id` (which may be an
 * index rather than a physical file).
 *
 * @param {Object} [options]
 * @returns {Promise<Array<{ name: string, id: string, type: string, provider: string, status: string }>>}
 * @throws {Error} If the listing format is invalid or any entry lacks a physical name
 */
export async function listAuthFiles(options = {}) {
  const endpoint = '/v0/management/auth-files';
  const res = await apiFetch(endpoint, options);
  const data = await res.json();
  if (!Array.isArray(data?.files)) {
    throw new Error('Invalid auth files listing: response missing "files" array.');
  }
  const rawFiles = data.files;

  const validFiles = [];
  for (let i = 0; i < rawFiles.length; i++) {
    const file = rawFiles[i];
    if (!file || typeof file !== 'object') {
      throw new Error(`Invalid auth files listing: entry at index ${i} is not a valid object.`);
    }
    if (typeof file.name !== 'string' || !file.name.trim()) {
      const identifier = file.id ? ` (id: "${String(file.id).trim()}")` : ` at index ${i}`;
      throw new Error(
        `Invalid auth files listing: entry${identifier} is missing a physical filename.`
      );
    }
    const exactName = file.name;

    validFiles.push({
      name: exactName,
      id: typeof file.id === 'string' && file.id.trim() ? file.id : exactName,
      type: typeof file.type === 'string' ? file.type.trim() : '',
      provider: typeof file.provider === 'string' ? file.provider.trim() : '',
      status: typeof file.status === 'string' ? file.status.trim() : '',
    });
  }

  return validFiles;
}

/**
 * Downloads raw auth file credential JSON and extracts ONLY the prefix.
 *
 * CRITICAL SECURITY INVARIANT:
 * Raw credential contents, tokens, client secrets, and refresh tokens are
 * discarded immediately and NEVER returned, stored in state, or logged.
 *
 * @param {string} fileName - Exact filename of the auth file
 * @param {Object} [options]
 * @returns {Promise<{ name: string, prefix: string }>}
 */
export async function downloadAuthFilePrefix(fileName, options = {}) {
  if (!fileName || typeof fileName !== 'string' || !fileName.trim()) {
    throw new Error('Invalid auth file name: filename must be a non-empty string');
  }

  const exactName = fileName;
  const encodedName = encodeURIComponent(exactName);
  const endpoint = `/v0/management/auth-files/download?name=${encodedName}`;

  const res = await apiFetch(endpoint, options);
  const rawData = await res.json();

  // Extract ONLY prefix. Discard entire rawData immediately.
  let prefix = '';
  if (rawData && typeof rawData.prefix === 'string') {
    prefix = rawData.prefix.trim();
  }

  return {
    name: exactName,
    prefix,
  };
}

/**
 * Fetches all auth files and their prefixes from the Management API.
 *
 * FAIL-CLOSED SAFETY CONTRACT:
 * If downloading ANY auth file's prefix fails (e.g. 404 Not Found, 401 Unauthorized),
 * this function REJECTS the entire load rather than returning prefix: '' which would
 * allow an unknown original prefix to be accidentally overwritten in a partially editable graph.
 *
 * @param {Object} [options]
 * @returns {Promise<Array<{ name: string, prefix: string }>>}
 * @throws {Error} Sanitized error with filename and HTTP status (no secrets)
 */
export async function loadAuthFilesWithPrefixes(options = {}) {
  const files = await listAuthFiles(options);
  if (!files.length) return [];

  // Fail closed: if ANY file fails to download, reject the entire batch
  const results = await Promise.all(
    files.map(async (file) => {
      try {
        const details = await downloadAuthFilePrefix(file.name, options);
        return {
          name: file.name,
          prefix: details.prefix,
        };
      } catch (err) {
        const safeName = sanitizeFileName(file.name);
        let statusText = 'request failed';
        if (err.message && err.message.includes('401')) {
          statusText = '401 Unauthorized';
        } else if (err.message && err.message.includes('404')) {
          statusText = '404 Not Found';
        } else if (err.message && err.message.includes('403')) {
          statusText = '403 Forbidden';
        } else {
          const match = err.message && err.message.match(/status (\d+)/i);
          if (match) {
            statusText = `HTTP ${match[1]}`;
          }
        }
        throw new Error(`Failed to load auth file "${safeName}": ${statusText}`);
      }
    })
  );

  // In CLIProxyAPI pool, duplicate prefixes are legitimate (account pools/rotation).
  // Return loaded results directly without blanket unique-prefix lockout.
  return results;
}

/**
 * Builds React Flow nodes and edges from actual auth files.
 *
 * SAFETY INVARIANTS:
 * - Node IDs are immutable exact auth-file names.
 * - Initial prefix and dirty tracking state are attached.
 * - Avoids leaking any credential keys (token, secret, client_secret, password, json) into state.
 * - Reconstructs parent/child edges when child prefixes match `${parentPrefix}_${n}`.
 * - Marks synthetic flag as false (real auth files).
 *
 * @param {Array<{ name: string, prefix: string }>} authFiles
 * @returns {{ nodes: Array<Object>, edges: Array<Object> }}
 */
export function buildGraphFromAuthFiles(authFiles = []) {
  if (!Array.isArray(authFiles) || !authFiles.length) {
    return { nodes: [], edges: [] };
  }

  // Filter and sanitize input records to ensure only clean name and prefix are consumed
  // Preserves exact physical filename identity
  const cleanFiles = [];
  authFiles.forEach((file) => {
    if (file && typeof file.name === 'string' && file.name.trim()) {
      cleanFiles.push({
        name: file.name,
        prefix: typeof file.prefix === 'string' ? file.prefix.trim() : '',
      });
    }
  });

  if (!cleanFiles.length) {
    return { nodes: [], edges: [] };
  }

  // Map prefix -> Array of auth files sharing that prefix
  // In CLIProxyAPI, duplicate prefixes are legitimate (pool rotation)
  const prefixToFilesMap = new Map();
  cleanFiles.forEach((file) => {
    if (file.prefix) {
      if (!prefixToFilesMap.has(file.prefix)) {
        prefixToFilesMap.set(file.prefix, []);
      }
      prefixToFilesMap.get(file.prefix).push(file);
    }
  });

  const edges = [];
  const childNodeIds = new Set();

  // Identify relationships where child prefix matches parent prefix + '_N'
  // When duplicate parent prefix exists, graph inference must NOT invent parent edge (leave ambiguous nodes disconnected)
  cleanFiles.forEach((file) => {
    const childPrefix = file.prefix;
    if (!childPrefix) return;

    const match = childPrefix.match(/^(.*)_(\d+)$/);
    if (match) {
      const candidateParentPrefix = match[1];
      const candidateParents = prefixToFilesMap.get(candidateParentPrefix) || [];
      // Only infer an edge when candidate parent prefix is unambiguous (exactly 1 parent file)
      if (candidateParents.length === 1) {
        const parentFile = candidateParents[0];
        if (parentFile.name !== file.name) {
          childNodeIds.add(file.name);
          edges.push({
            id: `edge__${parentFile.name}-${file.name}`,
            source: parentFile.name,
            target: file.name,
            animated: true,
            style: { stroke: '#58a6ff', strokeWidth: 2 },
          });
        }
      }
    }
  });

  // Build nodes with strictly whitelisted non-credential fields
  const nodes = cleanFiles.map((file, idx) => {
    const fileName = file.name;
    const prefix = file.prefix;
    const isChild = childNodeIds.has(fileName);
    const isRoot = !isChild && Boolean(prefix);

    // Layout in grid: roots on top rows, children on lower rows
    const col = idx % 4;
    const row = Math.floor(idx / 4);
    const position = {
      x: 100 + col * 240,
      y: isRoot ? 80 + row * 160 : 280 + row * 160,
    };

    return {
      id: fileName, // Immutable exact auth-file name
      type: 'profile',
      position,
      data: {
        fileName,
        prefix,
        initialPrefix: prefix,
        label: prefix || fileName,
        isDirty: false,
        isRoot,
        isSynthetic: false, // Real auth-file node
      },
    };
  });

  return { nodes, edges };
}

/**
 * Updates the prefix attribute of an auth record using PATCH /v0/management/auth-files/fields.
 *
 * CRITICAL SAFETY & SECURITY INVARIANTS:
 * - Only exact physical filename and current prefix (including empty string) are sent.
 * - Credentials, tokens, secrets, or keys are NEVER included in payload or logged.
 * - Enforces same-origin policy via apiFetch.
 * - Handles 401 Unauthorized, 404 Not Found, and 409 Conflict with sanitized error messages.
 *
 * @param {string} fileName - Exact physical filename of the auth file
 * @param {string} prefix - The new prefix string (empty string is explicitly allowed)
 * @param {Object} [options] - Options passed to apiFetch (e.g. key, fetchFn)
 * @returns {Promise<{ name: string, prefix: string, status: number }>}
 */
export async function patchAuthFilePrefix(fileName, prefix, options = {}) {
  if (!fileName || typeof fileName !== 'string' || !fileName.trim()) {
    throw new Error('Invalid auth file name: filename must be a non-empty string');
  }

  const exactName = fileName;
  const cleanPrefix = typeof prefix === 'string' ? prefix.trim() : '';

  const endpoint = '/v0/management/auth-files/fields';
  const body = JSON.stringify({
    name: exactName,
    prefix: cleanPrefix,
  });

  // SECURITY INVARIANTS:
  // 1. Disallow caller options from overriding HTTP method ('PATCH') or JSON body.
  // 2. Retain 'Content-Type: application/json' even if custom headers are requested.
  const {
    headers: customHeaders,
    method: _ignoredMethod,
    body: _ignoredBody,
    ...safeOptions
  } = options;

  const requestHeaders = {};
  if (customHeaders) {
    if (typeof customHeaders.forEach === 'function') {
      customHeaders.forEach((val, k) => {
        if (typeof k === 'string' && k.toLowerCase() !== 'content-type') {
          requestHeaders[k] = val;
        }
      });
    } else if (typeof customHeaders === 'object') {
      for (const [k, val] of Object.entries(customHeaders)) {
        if (typeof k === 'string' && k.toLowerCase() !== 'content-type') {
          requestHeaders[k] = val;
        }
      }
    }
  }

  // Ensure Content-Type is strictly application/json (case-insensitively delete any override attempts)
  for (const headerKey of Object.keys(requestHeaders)) {
    if (headerKey.toLowerCase() === 'content-type') {
      delete requestHeaders[headerKey];
    }
  }
  requestHeaders['Content-Type'] = 'application/json';

  try {
    const res = await apiFetch(endpoint, {
      ...safeOptions,
      method: 'PATCH',
      body,
      headers: requestHeaders,
    });

    return {
      name: exactName,
      prefix: cleanPrefix,
      status: res.status,
    };
  } catch (err) {
    const safeName = sanitizeFileName(exactName);
    const rawMsg = err.message || '';

    if (rawMsg.includes('401')) {
      throw new Error(`401 Unauthorized: Invalid or expired management key for "${safeName}"`);
    }
    if (rawMsg.includes('404')) {
      throw new Error(`404 Not Found: Auth file "${safeName}" not found`);
    }
    if (rawMsg.includes('409')) {
      throw new Error(`409 Conflict: Resource conflict for "${safeName}"`);
    }
    if (rawMsg.includes('403')) {
      throw new Error(
        `403 Forbidden: Management key does not have required permissions for "${safeName}"`
      );
    }

    throw new Error(`Failed to save prefix for "${safeName}": ${rawMsg || 'Request failed'}`);
  }
}

/**
 * Persists an array of dirty auth-file prefixes to the Management API.
 *
 * ATOMICITY & RECONCILIATION CONTRACT:
 * - Does NOT claim atomicity across multiple files, as the backend updates files individually.
 * - Rejects synthetic nodes: never sends synthetic nodes to the Management API.
 * - Permits duplicate prefixes for legitimate account rotation pools in CLIProxyAPI.
 * - Uses Promise.allSettled to track individual successes and failures without crashing on partial failure.
 * - Returns { successful: Array<{ name, prefix }>, failed: Array<{ name, prefix, error }> }.
 * - Errors are sanitized and contain no secret material or tokens.
 *
 * @param {Array<{ name: string, prefix: string, isSynthetic?: boolean }>} filesToSave
 * @param {Object} [options]
 * @returns {Promise<{ successful: Array<{ name: string, prefix: string }>, failed: Array<{ name: string, prefix: string, error: string }> }>}
 */
export async function saveAuthFilePrefixes(filesToSave = [], options = {}) {
  if (!Array.isArray(filesToSave) || filesToSave.length === 0) {
    return { successful: [], failed: [] };
  }

  // Filter out any synthetic items (never persist synthetic nodes)
  const realFiles = filesToSave.filter(
    (item) => item && typeof item.name === 'string' && !item.isSynthetic
  );

  if (realFiles.length === 0) {
    return { successful: [], failed: [] };
  }

  // In CLIProxyAPI pool, duplicate prefixes are legitimate (pool rotation)
  // No blanket unique-prefix lockout before sending requests
  const results = await Promise.allSettled(
    realFiles.map((item) => patchAuthFilePrefix(item.name, item.prefix, options))
  );

  const successful = [];
  const failed = [];

  results.forEach((res, idx) => {
    const item = realFiles[idx];
    if (res.status === 'fulfilled') {
      successful.push({
        name: item.name,
        prefix: typeof item.prefix === 'string' ? item.prefix.trim() : '',
      });
    } else {
      failed.push({
        name: item.name,
        prefix: typeof item.prefix === 'string' ? item.prefix.trim() : '',
        error: res.reason?.message || 'Save failed',
      });
    }
  });

  return { successful, failed };
}
