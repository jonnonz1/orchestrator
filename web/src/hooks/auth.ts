// Bearer-token authentication for the dashboard.
//
// On loopback-bound servers no token is required. When an operator binds a
// non-loopback address they must configure a bearer token; the dashboard
// reads it from localStorage.orchestratorToken on each request.
//
// Convenience: visiting the dashboard with `#token=<token>` in the URL stashes
// the token in localStorage and strips it from the URL so it doesn't sit in
// browser history or get shared in screenshots.

const STORAGE_KEY = 'orchestratorToken';

function captureHashToken() {
  if (typeof window === 'undefined') return;
  const hash = window.location.hash;
  if (!hash) return;
  const match = hash.match(/(?:^|[&#])token=([^&]+)/);
  if (!match) return;
  try {
    localStorage.setItem(STORAGE_KEY, decodeURIComponent(match[1]));
  } catch {
    // localStorage unavailable — silently skip.
  }
  // Strip the token from the URL so it doesn't live in history / screenshots.
  const cleanHash = hash
    .replace(/(?:^|[&#])token=[^&]+/, '')
    .replace(/^#&/, '#')
    .replace(/^#$/, '');
  const url =
    window.location.pathname + window.location.search + cleanHash;
  window.history.replaceState(null, '', url);
}

captureHashToken();

export function getToken(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string) {
  try {
    if (token) localStorage.setItem(STORAGE_KEY, token);
    else localStorage.removeItem(STORAGE_KEY);
  } catch {
    // localStorage unavailable — silently skip.
  }
}

/** Returns headers suitable for spreading into a fetch() call. */
export function authHeader(): Record<string, string> {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/**
 * Appends `?token=X` to a URL path when a token is stored. Needed for
 * WebSocket upgrades since browsers don't allow custom headers on
 * `new WebSocket()`. The server accepts ?token= as an alternative to
 * Authorization: Bearer.
 */
export function withTokenQuery(path: string): string {
  const token = getToken();
  if (!token) return path;
  const sep = path.includes('?') ? '&' : '?';
  return `${path}${sep}token=${encodeURIComponent(token)}`;
}
