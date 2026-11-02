import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import OwnerNav from '../components/OwnerNav.jsx';
import AnalyticsLineChart from '../components/AnalyticsLineChart.jsx';
import ConfirmDialog from '../components/ConfirmDialog.jsx';
import useDashboardLive from '../hooks/useDashboardLive.js';
import useRelativeTimeClock from '../hooks/useRelativeTimeClock.js';
import formatRelativeTime from '../utils/formatRelativeTime.js';

export default function PollAnalyticsPage() {
  const { slug } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [toast, setToast] = useState(() => location.state?.toast || '');
  const [pendingAction, setPendingAction] = useState('');
  const clock = useRelativeTimeClock();

  const load = useCallback(async () => {
    try {
      const response = await apiRequest(`/api/polls/${encodeURIComponent(slug)}/analytics`);
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || 'Unable to load analytics.');
      setData(payload);
      setError('');
    } catch (loadError) {
      setError(loadError.message || 'Unable to load analytics.');
    }
  }, [slug]);

  const liveStatus = useDashboardLive(load);
  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    const canonicalSlug = data?.poll?.slug;
    if (canonicalSlug && canonicalSlug !== slug) {
      navigate(`/polls/${encodeURIComponent(canonicalSlug)}`, { replace: true, state: location.state });
    }
  }, [data?.poll?.slug, slug, navigate, location.state]);
  useEffect(() => {
    if (liveStatus === 'live') return undefined;
    const timer = window.setInterval(load, 4000);
    return () => window.clearInterval(timer);
  }, [liveStatus, load]);
  useEffect(() => {
    if (!location.state?.toast) return undefined;
    const timer = window.setTimeout(() => setToast(''), 3000);
    return () => window.clearTimeout(timer);
  }, [location.key, location.state?.toast]);

  const total = Number(data?.totalVotes || 0);
  const views = Number(data?.views || 0);
  const shareLink = data?.poll?.slug ? `${window.location.origin}/p/${data.poll.slug}` : '';
  const recentVotes = Array.isArray(data?.recentVotes) ? data.recentVotes.slice(0, 5) : [];

  const cancelConfirmation = useCallback(() => setPendingAction(''), []);

  async function performAction() {
    const action = pendingAction;
    setPendingAction('');
    const response = action === 'close'
    ? await apiRequest(`/api/polls/${encodeURIComponent(data?.poll?.slug || slug)}/close`, { method: 'PATCH' })
      : await apiRequest(`/api/polls/${encodeURIComponent(data?.poll?.slug || slug)}`, { method: 'DELETE' });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) { setError(payload.error || `Unable to ${action} poll.`); return; }
    if (action === 'delete') { navigate('/polls', { replace: true }); return; }
    setToast('Poll closed.');
    load();
    window.setTimeout(() => setToast(''), 3000);
  }

  async function copyLink() {
    try {
      await navigator.clipboard.writeText(shareLink);
      setToast('Share link copied.');
      window.setTimeout(() => setToast(''), 3000);
    } catch { setError('Could not copy the share link.'); }
  }

  return <div className="owner-app"><OwnerNav/><main className="owner-main poll-analytics-main">
    <Link className="owner-back-link" to="/polls">← Back to polls</Link>
    {error && <div className="inline-error" role="alert"><span>{error}</span><button className="button button-secondary" type="button" onClick={() => { setError(''); load(); }}>Retry</button></div>}
    {!data && !error && <div className="analytics-skeleton" aria-label="Loading analytics"><div/><div/><div/><div/></div>}
    {data && <>
      <section className="poll-analytics-header">
        <div className="analytics-heading-copy">
          <p className="eyebrow">POLL ANALYTICS</p><h1>{data.poll.question}</h1>
          <div className="poll-analytics-share"><span className={`status-badge status-${data.poll.status}`}>{data.poll.status}</span><code>{shareLink}</code><button className="button button-secondary" type="button" onClick={copyLink}>Copy link</button></div>
        </div>
        <div className="poll-header-actions">{data.poll.status === 'active' && <button className="button button-secondary" type="button" onClick={() => setPendingAction('close')}>Close poll</button>}<button className="button button-danger" type="button" onClick={() => setPendingAction('delete')}>Delete</button></div>
      </section>
      <div className="analytics-live-row"><span className={`live-badge live-${liveStatus}`}><i/> {liveStatus === 'live' ? 'Live' : liveStatus === 'reconnecting' ? 'Reconnecting' : 'Connecting'}</span></div>
      <div className="analytics-grid poll-analytics-columns">
        <div className="poll-analytics-stack">
          <section className="dashboard-card">
            <div className="section-heading"><div><h2>Engagement</h2><p>Poll activity at a glance</p></div></div>
            <div className="poll-engagement-metrics" aria-label="Poll engagement statistics">
              <article><strong>{views.toLocaleString()}</strong><span>Views</span></article>
              <article><strong>{total.toLocaleString()}</strong><span>Votes</span></article>
              <article><strong>{views ? `${Math.round(Number(data.conversionRate || 0))}%` : '—'}</strong><span>Vote rate</span></article>
            </div>
          </section>
          <section className="dashboard-card">
            <div className="section-heading"><div><h2>Recent votes</h2><p>Latest responses</p></div></div>
            {recentVotes.length ? <ul className="poll-recent-votes">{recentVotes.map((vote, index) => <li key={`${vote.createdAt}-${index}`}><span>{vote.optionName}</span><time dateTime={vote.createdAt}>{formatRelativeTime(vote.createdAt, clock)}</time></li>)}</ul> : <p className="poll-recent-empty">No votes yet</p>}
          </section>
        </div>
        <div className="poll-analytics-stack">
          <section className="dashboard-card">
            <div className="section-heading"><div><h2>Option breakdown</h2><p>{total.toLocaleString()} total votes</p></div></div>
            <div className="results-legend">{data.options.map((option, index) => <div className="results-legend-row" key={option.id}><i className={`legend-dot color-${index % 6}`}/><div className="results-option-copy"><div><span>{option.text}</span><strong>{Number(option.votes || 0).toLocaleString()} <small>{Math.round(Number(option.percentage || 0))}%</small></strong></div><i className="results-track"><b className={`data-bar color-${index % 6}`} style={{ width: `${Math.min(100, Number(option.percentage || 0))}%` }}/></i></div></div>)}</div>
          </section>
          {total >= 10
            ? <section className="dashboard-card"><div className="section-heading"><div><h2>Votes over time</h2><p>Hourly votes · last 24 hours (UTC)</p></div></div><AnalyticsLineChart data={data.votesOverTime} height={280}/></section>
            : <section className="dashboard-card poll-trend-empty"><div className="section-heading"><div><h2>Votes over time</h2></div></div><p>Not enough votes yet for a trend</p></section>}
        </div>
      </div>
      {toast && <div className="owner-toast" role="status">{toast}</div>}
    </>}
  </main>{pendingAction && <ConfirmDialog title={pendingAction === 'close' ? 'Close this poll?' : 'Delete this poll?'} message={pendingAction === 'close' ? 'The poll will stop accepting new responses.' : 'The poll, its votes, and analytics will be permanently deleted.'} confirmLabel={pendingAction === 'close' ? 'Close poll' : 'Delete poll'} danger={pendingAction === 'delete'} onCancel={cancelConfirmation} onConfirm={performAction}/>}</div>;
}
