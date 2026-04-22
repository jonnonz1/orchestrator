import { useState, useEffect, useCallback } from 'react';
import { authHeader, getToken } from './auth';

const BASE = '/api/v1';

export function useApi<T>(path: string, interval?: number) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await fetch(BASE + path, { headers: authHeader() });
      if (res.status === 401) {
        throw new Error('unauthorized — set the bearer token (see README)');
      }
      if (!res.ok) throw new Error(await res.text());
      setData(await res.json());
      setError(null);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [path]);

  useEffect(() => {
    refresh();
    if (interval) {
      const id = setInterval(refresh, interval);
      return () => clearInterval(id);
    }
  }, [refresh, interval]);

  return { data, loading, error, refresh };
}

export async function apiPost<T>(path: string, body: any): Promise<T> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeader() },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  return res.json();
}

export async function apiDelete(path: string): Promise<void> {
  const res = await fetch(BASE + path, {
    method: 'DELETE',
    headers: authHeader(),
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(res.statusText);
  }
}

// Re-export for convenience
export { getToken };
