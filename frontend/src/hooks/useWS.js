import { useEffect, useRef } from 'react';
import { storage } from '../api';

export function useWS(path, onMessage) {
  const wsRef = useRef(null);
  useEffect(() => {
    if (!path) return undefined;
    const base = storage.base.replace(/^http/, 'ws').replace(/\/+$/, '');
    const url = base + path;
    let ws;
    try {
      ws = new WebSocket(url);
      wsRef.current = ws;
    } catch (e) {
      return undefined;
    }
    ws.onmessage = (evt) => {
      if (!onMessage) return;
      try { onMessage(JSON.parse(evt.data)); } catch { /* ignore */ }
    };
    return () => {
      if (ws) ws.close();
    };
  }, [path, onMessage]);
  return wsRef.current;
}
