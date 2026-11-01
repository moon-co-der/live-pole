import { useEffect, useRef, useState } from 'react';
import { apiRequest, webSocketURL } from '../api/client.js';

export default function useLivePoll(slug, enabled) {
  const [live, setLive] = useState({ status: 'idle', question: '', options: [], counts: {}, total: 0 });
  const closedRef = useRef(false);
  useEffect(() => {
    if (!slug || !enabled) {
      setLive({ status: 'idle', question: '', options: [], counts: {}, total: 0 });
      return undefined;
    }
    let socket;
    let timer;
    let stableTimer;
    let attempts = 0;
    let stopped = false;
    closedRef.current = false;
    setLive({ status: 'connecting', question: '', options: [], counts: {}, total: 0 });
    const connect = () => {
      if (stopped || closedRef.current) return;
      setLive((v) => ({ ...v, status: attempts ? 'reconnecting' : 'connecting' }));
      (async () => {
        try {
          const response = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}/live-ticket`, { method: 'POST' });
          const payload = await response.json().catch(() => ({}));
          if (!response.ok || !payload.ticket || stopped) throw new Error('Unable to authorize live results.');
          const url = new URL(webSocketURL(`/ws/polls/${encodeURIComponent(slug)}`));
          url.searchParams.set('ticket', payload.ticket);
          socket = new WebSocket(url.toString());
          attachSocket();
        } catch {
          if (!stopped) setLive((v) => ({ ...v, status: 'reconnecting' }));
          if (!stopped) timer = window.setTimeout(connect, 3000);
        }
      })();
    };
    const attachSocket = () => {
      socket.onopen = () => { setLive((v) => ({ ...v, status: 'live' })); stableTimer = window.setTimeout(() => { attempts = 0; }, 5000); };
      socket.onmessage = ({ data }) => {
        let message;
        try { message = JSON.parse(data); } catch { return; }
        if (message.type === 'snapshot') setLive({ status: 'live', question: message.question || '', options: message.options || [], counts: message.counts || {}, total: message.total || 0 });
        if (message.type === 'update') setLive((v) => ({ ...v, status: 'live', counts: message.counts || v.counts, total: message.total ?? v.total }));
        if (message.type === 'poll_closed') { closedRef.current = true; setLive((v) => ({ ...v, status: 'closed', counts: message.counts || v.counts, total: message.total ?? v.total })); socket?.close(); }
      };
      socket.onclose = () => {
        window.clearTimeout(stableTimer);
        if (stopped || closedRef.current) return;
        attempts += 1;
        const base = Math.min(30000, 1000 * (2 ** Math.min(attempts - 1, 5)));
        const delay = Math.round(base * (0.8 + Math.random() * 0.4));
        setLive((v) => ({ ...v, status: 'reconnecting' }));
        timer = window.setTimeout(connect, delay);
      };
      socket.onerror = () => socket?.close();
    };
    connect();
    return () => { stopped = true; window.clearTimeout(timer); window.clearTimeout(stableTimer); socket?.close(); };
  }, [slug, enabled]);
  return live;
}
