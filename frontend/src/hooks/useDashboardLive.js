import { useEffect, useRef, useState } from 'react';
import { apiRequest, webSocketURL } from '../api/client.js';

export default function useDashboardLive(onUpdate) {
  const callback = useRef(onUpdate);
  const [status, setStatus] = useState('connecting');
  useEffect(() => { callback.current = onUpdate; }, [onUpdate]);
  useEffect(() => {
    let stopped = false;
    let socket;
    let reconnectTimer;
    let debounceTimer;
    let attempts = 0;
    const connect = async () => {
      if (stopped) return;
      try {
        setStatus(attempts ? 'reconnecting' : 'connecting');
        const response = await apiRequest('/api/ws/ticket', { method: 'POST' });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(payload.error || 'Ticket request failed');
        if (stopped) return;
        socket = new WebSocket(webSocketURL(`/ws/dashboard?ticket=${encodeURIComponent(payload.ticket)}`));
        socket.onopen = () => { attempts = 0; setStatus('live'); };
        socket.onmessage = ({ data }) => {
          try { const event = JSON.parse(data); if (event.type === 'dashboard:update') { window.clearTimeout(debounceTimer); debounceTimer = window.setTimeout(() => callback.current?.(event), 1000); } } catch { /* Ignore malformed events. */ }
        };
        socket.onclose = () => {
          if (stopped) return;
          attempts += 1; setStatus('reconnecting');
          const delay = Math.min(30000, 1000 * (2 ** Math.min(attempts - 1, 5))) * (0.8 + Math.random() * 0.4);
          reconnectTimer = window.setTimeout(connect, delay);
        };
        socket.onerror = () => socket?.close();
      } catch {
        if (!stopped) { attempts += 1; setStatus('reconnecting'); reconnectTimer = window.setTimeout(connect, Math.min(30000, 1000 * (2 ** Math.min(attempts - 1, 5)))); }
      }
    };
    connect();
    return () => { stopped = true; window.clearTimeout(reconnectTimer); window.clearTimeout(debounceTimer); socket?.close(); };
  }, []);
  return status;
}
