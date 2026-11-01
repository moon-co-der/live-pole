import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import OwnerNav from '../components/OwnerNav.jsx';
import AnalyticsLineChart from '../components/AnalyticsLineChart.jsx';
import ConfirmDialog from '../components/ConfirmDialog.jsx';
import useDashboardLive from '../hooks/useDashboardLive.js';

const donutCircumference = 2 * Math.PI * 40;

export default function PollAnalyticsPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [toast, setToast] = useState(() => location.state?.toast || '');
  const [pendingAction, setPendingAction] = useState('');
  const load = useCallback(async () => {
    try {
      const response = await apiRequest(`/api/polls/${encodeURIComponent(id)}/analytics`);
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || 'Unable to load analytics.');
      setData(payload); setError('');
    } catch (loadError) { setError(loadError.message || 'Unable to load analytics.'); }
  }, [id]);
  const liveStatus = useDashboardLive(load);
  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    if (!location.state?.toast) return undefined;
    const timer = window.setTimeout(() => setToast(''), 3000);
    return () => window.clearTimeout(timer);
  }, [location.key, location.state?.toast]);
  const total = data?.totalVotes || 0;
  const shareLink = data?.poll?.slug ? `${window.location.origin}/p/${data.poll.slug}` : '';
  const donutSegments = [];
  if (total && data?.options) {
    let passed = 0;
    for (const [index, option] of data.options.entries()) {
      const portion = Number(option.votes || 0) / total;
      donutSegments.push({
        index: index % 2,
        length: donutCircumference * portion,
        offset: -donutCircumference * passed,
      });
      passed += portion;
    }
  }
  const cancelConfirmation = useCallback(() => setPendingAction(''), []);

  async function performAction() {
    const action = pendingAction;
    setPendingAction('');
    const response = action === 'close'
      ? await apiRequest(`/api/polls/${id}/close`, { method: 'PATCH' })
      : await apiRequest(`/api/polls/${id}`, { method: 'DELETE' });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) { setError(payload.error || `Unable to ${action} poll.`); return; }
    if (action === 'delete') { navigate('/polls', { replace: true }); return; }
    setToast('Poll closed.');
    load();
    window.setTimeout(() => setToast(''), 3000);
  }

  async function copyLink() {
    try { await navigator.clipboard.writeText(shareLink); setToast('Share link copied.'); window.setTimeout(() => setToast(''), 3000); }
    catch { setError('Could not copy the share link.'); }
  }

  return <div className="owner-app"><OwnerNav/><main className="owner-main poll-analytics-main">
    <Link className="owner-back-link" to="/polls">← Back to polls</Link>
    {error && <div className="inline-error" role="alert"><span>{error}</span><button className="button button-secondary" type="button" onClick={() => { setError(''); load(); }}>Retry</button></div>}
    {!data && !error && <div className="analytics-skeleton" aria-label="Loading analytics"><div/><div/><div/><div/></div>}
    {data && <>
      <section className="poll-analytics-header"><div className="analytics-heading-copy"><p className="eyebrow">POLL ANALYTICS</p><h1>{data.poll.question}</h1><div className="poll-analytics-share"><span className={`status-badge status-${data.poll.status}`}>{data.poll.status}</span><code>{shareLink}</code><button className="button button-secondary" type="button" onClick={copyLink}>Copy link</button></div></div><div className="poll-header-actions">{data.poll.status === 'active' && <button className="button button-secondary" type="button" onClick={() => setPendingAction('close')}>Close poll</button>}<button className="button button-danger" type="button" onClick={() => setPendingAction('delete')}>Delete</button></div></section>
      <div className="analytics-live-row"><span className={`live-badge live-${liveStatus}`}><i/> {liveStatus === 'live' ? 'Live' : liveStatus === 'reconnecting' ? 'Reconnecting' : 'Connecting'}</span></div>
      <section className="analytics-stat-grid" aria-label="Poll statistics"><article><span>Total votes</span><strong>{total.toLocaleString()}</strong></article><article><span>Views</span><strong>{Number(data.views || 0).toLocaleString()}</strong></article><article><span>Conversion rate</span><strong>{Math.round(Number(data.conversionRate || 0))}%</strong></article><article><span>Poll status</span><strong className="analytics-stat-status">{data.poll.status}</strong></article></section>
      {total === 0 ? <section className="dashboard-card poll-no-votes"><span aria-hidden="true">◉</span><h2>No votes yet</h2><p>Share your poll to start collecting responses. Your analytics will appear here.</p><button className="button button-secondary" type="button" onClick={copyLink}>Copy share link</button></section> : <>
      <div className="analytics-grid analytics-results-grid"><section className="dashboard-card"><div className="section-heading"><div><h2>Results by option</h2><p>{total.toLocaleString()} total responses</p></div></div><div className="results-overview"><div className="results-donut" role="img" aria-label="Vote distribution by option"><svg viewBox="0 0 100 100" aria-hidden="true"><circle className="donut-track" cx="50" cy="50" r="40"/>{donutSegments.map((segment, index) => <circle className={`donut-segment color-${segment.index}`} key={`${segment.index}-${index}`} cx="50" cy="50" r="40" transform="rotate(-90 50 50)" style={{ strokeDasharray: `${segment.length} ${donutCircumference}`, strokeDashoffset: segment.offset }}/>)}</svg><div className="donut-center"><strong>{total.toLocaleString()}</strong><span>votes</span></div></div><div className="results-legend">{data.options.map((option, index) => <div className={`results-legend-row${option.winner ? ' is-winner' : ''}`} key={option.id}><i className={`legend-dot color-${index % 2}`}/><div className="results-option-copy"><div><span>{option.text}{option.winner && <b className="winner-label">Winner</b>}</span><strong>{Number(option.votes || 0).toLocaleString()} <small>{Math.round(Number(option.percentage || 0))}%</small></strong></div><i className="results-track"><b className={`data-bar color-${index % 2}`} style={{ width: `${option.percentage}%` }}/></i></div></div>)}</div></div></section>
          <section className="dashboard-card"><div className="section-heading"><div><h2>Votes over time</h2><p>Responses by day</p></div></div><AnalyticsLineChart data={data.votesOverTime} height={280}/></section></div>
        <div className="analytics-grid"><section className="dashboard-card"><div className="section-heading"><div><h2>Peak voting hours</h2><p>Times shown in UTC</p></div></div><div className="hour-bars">{Array.from({ length: 24 }, (_, hour) => { const count = data.peakHours.find((item) => Number(item.label) === hour)?.count || 0; const max = Math.max(1, ...data.peakHours.map((item) => item.count)); return <div className="hour-bar" key={hour} title={`${hour}:00 · ${count} votes`}><i style={{ '--bar-height': `${Math.max(count ? 8 : 2, count / max * 100)}%` }}/><span>{hour % 3 === 0 ? String(hour).padStart(2, '0') : ''}</span></div>; })}</div></section>
          <section className="dashboard-card split-card"><div className="section-heading"><div><h2>Devices & traffic source</h2><p>Where responses came from</p></div></div><h3>Devices</h3>{data.deviceSplit.map((item) => <SplitBar key={item.label} item={item} total={total}/>)}<h3 className="split-title-spaced">Traffic source</h3>{data.sourceSplit.map((item) => <SplitBar key={item.label} item={item} total={total}/>)}</section></div>
      </>}
      {toast && <div className="owner-toast" role="status">{toast}</div>}
    </>}
  </main>{pendingAction && <ConfirmDialog title={pendingAction === 'close' ? 'Close this poll?' : 'Delete this poll?'} message={pendingAction === 'close' ? 'The poll will stop accepting new responses.' : 'The poll, its votes, and analytics will be permanently deleted.'} confirmLabel={pendingAction === 'close' ? 'Close poll' : 'Delete poll'} danger={pendingAction === 'delete'} onCancel={cancelConfirmation} onConfirm={performAction}/>}</div>;
}

function SplitBar({ item, total }) {
  const percent = total ? Math.round(item.count / total * 100) : 0;
  return <div className="split-bar-row"><div><span>{item.label}</span><strong>{Number(item.count || 0).toLocaleString()} <small>{percent}%</small></strong></div><i><b style={{ width: `${percent}%` }}/></i></div>;
}
