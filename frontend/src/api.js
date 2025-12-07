import { useEffect, useState } from 'react';

export const storage = {
  get base() { return localStorage.getItem('peerwan_base') || window.location.origin || 'http://127.0.0.1:8080'; },
  set base(v) { localStorage.setItem('peerwan_base', v); },
  get token() { return localStorage.getItem('peerwan_token') || ''; },
  set token(v) { localStorage.setItem('peerwan_token', v); },
  get adminSet() { return localStorage.getItem('peerwan_admin_set') === 'true'; },
  set adminSet(v) { localStorage.setItem('peerwan_admin_set', v ? 'true' : 'false'); },
};

export async function api(path, options = {}) {
  const base = (options.base || storage.base).replace(/\/+$/, '');
  const headers = Object.assign({}, options.headers || {});
  const token = storage.token;
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch(base + path, { ...options, headers });
  if (res.status === 401) {
    throw { unauthorized: true };
  }
  if (!res.ok) {
    const text = await res.text();
    throw new Error(path + ' ' + res.status + ' ' + text);
  }
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/json')) return res.json();
  return res.text();
}

export function useAsync(fn, deps = []) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [data, setData] = useState(null);
  const run = async (...args) => {
    setLoading(true); setError(null);
    try { const d = await fn(...args); setData(d); return d; }
    catch (e) { setError(e); throw e; }
    finally { setLoading(false); }
  };
  useEffect(()=>{ run(); }, deps); // eslint-disable-line react-hooks/exhaustive-deps
  return { loading, error, data, run };
}
