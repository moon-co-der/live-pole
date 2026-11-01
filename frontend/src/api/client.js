const apiBaseURL = (import.meta.env.VITE_API_URL || '').replace(/\/+$/, '');
export const tokenStorageKey = 'livepoll_token';
const voterStorageKey = 'livepoll_voter_id';

function createVoterUUID() {
  if (crypto.randomUUID) return crypto.randomUUID();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function voterUUID() {
  let value = localStorage.getItem(voterStorageKey);
  if (!value) {
    value = createVoterUUID();
    localStorage.setItem(voterStorageKey, value);
  }
  return value;
}

export function webSocketURL(path) {
  if (!apiBaseURL) {
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${scheme}//${window.location.host}${path}`;
  }
  const url = new URL(apiBaseURL);
  const websocketProtocol = { 'http:': 'ws:', 'https:': 'wss:' }[url.protocol];
  if (!websocketProtocol) throw new Error('VITE_API_URL must use http or https.');
  url.protocol = websocketProtocol;
  url.pathname = `${url.pathname.replace(/\/+$/, '')}${path}`;
  return url.toString();
}

export async function apiRequest(path, options = {}) {
  const url = `${apiBaseURL}${path.startsWith('/') ? path : `/${path}`}`;
  const headers = new Headers(options.headers || {});
  if (/^\/api\/public\/polls\//.test(path)) headers.set('X-Voter-Id', voterUUID());
  const token = localStorage.getItem(tokenStorageKey);
  if (token && !headers.has('Authorization')) headers.set('Authorization', `Bearer ${token}`);
  const response = await fetch(url, { credentials: 'include', ...options, headers });
  if (response.status === 401) {
    localStorage.removeItem(tokenStorageKey);
    if (window.location.pathname !== '/login') {
      window.location.assign('/login');
    }
  }
  return response;
}
