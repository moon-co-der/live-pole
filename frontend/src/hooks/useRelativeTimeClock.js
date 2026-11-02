import { useEffect, useState } from 'react';

export default function useRelativeTimeClock() {
  const [now, setNow] = useState(Date.now);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);

  return now;
}
