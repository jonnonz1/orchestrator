import { useState, useEffect, useRef } from 'react';
import type { WSMessage } from '../types';
import { withTokenQuery } from './auth';

export function useWebSocket(url: string | null) {
  const [messages, setMessages] = useState<WSMessage[]>([]);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    if (!url) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    // Browsers disallow custom headers on WebSocket upgrades, so the bearer
    // token (if any) is forwarded as `?token=<token>` — the server accepts
    // either the Authorization header or this query param.
    const wsUrl = `${protocol}//${window.location.host}${withTokenQuery(url)}`;
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        setMessages((prev) => [...prev, msg]);
      } catch {}
    };

    return () => {
      ws.close();
      wsRef.current = null;
    };
  }, [url]);

  return { messages, connected };
}
