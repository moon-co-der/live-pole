export default function formatRelativeTime(timestamp, now = Date.now()) {
  const value = typeof timestamp === 'number' ? timestamp : Date.parse(timestamp);
  if (!Number.isFinite(value)) return 'Just now';

  const elapsed = Math.max(0, now - value);
  if (elapsed < 60_000) return 'Just now';
  if (elapsed < 60 * 60_000) return `${Math.floor(elapsed / 60_000)}m ago`;
  if (elapsed < 24 * 60 * 60_000) return `${Math.floor(elapsed / (60 * 60_000))}h ago`;
  return `${Math.floor(elapsed / (24 * 60 * 60_000))}d ago`;
}
