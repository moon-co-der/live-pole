import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import OwnerNav from '../components/OwnerNav.jsx';
import CreatePollFab from '../components/CreatePollFab.jsx';
import useDashboardLive from '../hooks/useDashboardLive.js';
import useRelativeTimeClock from '../hooks/useRelativeTimeClock.js';
import { useAuth } from '../context/AuthContext.jsx';
import formatRelativeTime from '../utils/formatRelativeTime.js';

const empty = {
  totalPolls: { value: 0 },
  totalVotes: { value: 0 },
  openPolls: 0,
  votesToday: 0,
  pollsWithoutVotes: 0,
  polls: [],
  recentActivity: [],
};

function displayName(email) {
  const raw = email?.split('@')[0]?.replace(/[0-9._]/g, '') || 'there';
  return raw.charAt(0).toUpperCase() + raw.slice(1);
}

export default function DashboardPage() {
  const [data, setData] = useState(empty);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [copyingPoll, setCopyingPoll] = useState('');
  const [copyMessage, setCopyMessage] = useState('');
  const clock = useRelativeTimeClock();
  const { user } = useAuth();

  const load = useCallback(async () => {
    try {
      const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
      const [overviewResponse, pollsResponse] = await Promise.all([
        apiRequest(`/api/analytics/overview?timezone=${encodeURIComponent(timezone)}`, { cache: 'no-store' }),
        apiRequest('/api/polls?page=1&limit=8&sort=newest', { cache: 'no-store' }),
      ]);
      const [payload, pollsPayload] = await Promise.all([
        overviewResponse.json().catch(() => ({})),
        pollsResponse.json().catch(() => ({})),
      ]);
      if (!overviewResponse.ok) throw new Error(payload.error || 'Unable to load dashboard data.');
      if (!pollsResponse.ok) throw new Error(pollsPayload.error || 'Unable to load your polls.');
      const pollCount = Number(pollsPayload.pagination?.total);
      setData({
        ...empty,
        ...payload,
        totalPolls: Number.isFinite(pollCount) ? { ...(payload.totalPolls || {}), value: pollCount } : payload.totalPolls || empty.totalPolls,
        polls: Array.isArray(pollsPayload.polls) ? pollsPayload.polls : [],
        recentActivity: payload.recentActivity || [],
      });
      setError('');
    } catch (loadError) {
      setError(loadError.message || 'Unable to load dashboard.');
    } finally {
      setLoading(false);
    }
  }, []);

  const liveStatus = useDashboardLive(load);
  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    if (liveStatus === 'live') return undefined;
    const timer = window.setInterval(load, 4000);
    return () => window.clearInterval(timer);
  }, [liveStatus, load]);
  const stats = [
    ['Total polls', Number(data.totalPolls?.value || 0)],
    ['Total votes', Number(data.totalVotes?.value || 0)],
    ['Open polls', Number(data.openPolls || 0)],
    ['Votes today', Number(data.votesToday || 0)],
  ];

  async function copyPollLink(poll) {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}/p/${poll.slug}`);
      setCopyingPoll(poll.id);
      setCopyMessage('Poll link copied.');
      window.setTimeout(() => setCopyingPoll((current) => current === poll.id ? '' : current), 1800);
      window.setTimeout(() => setCopyMessage(''), 1800);
    } catch {
      setCopyMessage('Could not copy the poll link.');
    }
  }

  return <div className="owner-app"><OwnerNav/><main className="owner-main dashboard-main">
    <header className="page-heading"><div><h1>Welcome back, {displayName(user?.email)}</h1><p>Here's what's happening with your polls.</p></div><Link className="button button-primary page-create-button" to="/create"><span aria-hidden="true">+</span> Create poll</Link></header>
    {error && <div className="inline-error" role="alert"><span>{error}</span><button className="button button-secondary" type="button" onClick={() => { setLoading(true); load(); }}>Retry</button></div>}
    {loading ? <div className="dashboard-stats creator-dashboard-stats" aria-label="Loading dashboard">{stats.map(([label]) => <article className="dashboard-stat skeleton-card" key={label}><span className="skeleton-line"/><span className="skeleton-number"/></article>)}</div> : <>
      <section className="dashboard-stats creator-dashboard-stats" aria-label="Poll statistics" aria-live="polite">{stats.map(([label, value]) => <article className="dashboard-stat creator-dashboard-stat" key={label}><span className="creator-stat-label">{label}</span><strong>{value.toLocaleString()}</strong></article>)}</section>
      {Number(data.pollsWithoutVotes || 0) > 0 && <p className="dashboard-needs-attention">{Number(data.pollsWithoutVotes).toLocaleString()} {Number(data.pollsWithoutVotes) === 1 ? 'poll has' : 'polls have'} no votes yet. Share the link.</p>}
      <div className="dashboard-columns creator-dashboard-columns">
        <section className="dashboard-card creator-dashboard-card">
          <div className="section-heading"><div><h2>Your polls</h2><p>Your newest polls and their latest activity.</p></div><Link className="text-link" to="/polls">View all</Link></div>
          {data.polls.length ? <ul className="creator-poll-list">{data.polls.slice(0, 8).map((poll) => <li className="creator-poll-row" key={poll.id}>
            <div className="creator-poll-main"><Link className="creator-poll-title" to={`/polls/${poll.slug}`}>{poll.question}</Link><span className={`status-badge status-${poll.status}`}>{poll.status === 'active' ? 'Open' : 'Closed'}</span></div>
            <div className="creator-poll-count"><strong>{Number(poll.totalVotes || 0).toLocaleString()}</strong><span>{Number(poll.totalVotes || 0) === 1 ? 'vote' : 'votes'}</span></div>
            {poll.lastVoteAt && <span className="creator-poll-last-vote">Last vote {formatRelativeTime(poll.lastVoteAt, clock)}</span>}
            <button className="button button-secondary creator-copy-link" type="button" onClick={() => copyPollLink(poll)}>{copyingPoll === poll.id ? 'Copied' : 'Copy link'}</button>
          </li>)}</ul> : <div className="compact-empty creator-polls-empty"><span aria-hidden="true">▤</span><strong>No polls yet</strong><p>Create your first poll to get started.</p><Link className="button button-secondary" to="/create">Create your first poll</Link></div>}
          <p className="sr-only" role="status" aria-live="polite">{copyMessage}</p>
        </section>
        <section className="dashboard-card creator-dashboard-card">
          <div className="section-heading"><div><h2>Recent activity</h2><p>The latest votes across your polls.</p></div><span className={`live-badge live-${liveStatus}`}><i/> {liveStatus === 'live' ? 'Live' : liveStatus === 'reconnecting' ? 'Reconnecting' : 'Connecting'}</span></div>
          {data.recentActivity.length ? <ul className="creator-activity-list">{data.recentActivity.slice(0, 5).map((activity, index) => <li key={`${activity.createdAt}-${index}`}><span>{activity.optionName} <small>on</small> {activity.pollTitle}</span><time dateTime={activity.createdAt}>· {formatRelativeTime(activity.createdAt, clock)}</time></li>)}</ul> : <p className="creator-activity-empty">No votes yet</p>}
        </section>
      </div>
    </>}
  </main><CreatePollFab/></div>;
}
