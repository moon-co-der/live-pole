import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import OwnerNav from '../components/OwnerNav.jsx';
import AnalyticsLineChart from '../components/AnalyticsLineChart.jsx';
import CreatePollFab from '../components/CreatePollFab.jsx';
import useDashboardLive from '../hooks/useDashboardLive.js';
import { useAuth } from '../context/AuthContext.jsx';

const empty = { overview: null, series: [], top: [], recent: [] };
const statIcons = {
  polls: <><rect x="4" y="5" width="16" height="15" rx="2"/><path d="M8 9h8M8 13h5"/></>,
  votes: <><path d="M5 19V9M12 19V5M19 19v-7"/><path d="M3 19h18"/></>,
  views: <><path d="M2.5 12s3.4-6 9.5-6 9.5 6 9.5 6-3.4 6-9.5 6-9.5-6-9.5-6Z"/><circle cx="12" cy="12" r="2.5"/></>,
  engagement: <><path d="M20 7 10 17l-5-5"/><path d="M20 12v6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h9"/></>,
};

function displayName(email) {
  const raw = email?.split('@')[0]?.replace(/[0-9._]/g, '') || 'there';
  return raw.charAt(0).toUpperCase() + raw.slice(1);
}

function Growth({ stat }) {
  const growth = Number(stat?.growth || 0);
  if (!stat || growth === 100) return <span className="growth-chip growth-new">New</span>;
  const direction = growth > 0 ? 'up' : growth < 0 ? 'down' : 'flat';
  return <span className={`growth-chip growth-${direction}`}><span aria-hidden="true">{direction === 'up' ? '↑' : direction === 'down' ? '↓' : '–'}</span> {Math.abs(Math.round(growth))}%</span>;
}

function timeAgo(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const days = Math.max(0, Math.floor((Date.now() - date.getTime()) / 86400000));
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  if (days < 30) return `${days} days ago`;
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

function PollPreviewCard({ poll }) {
  const total = Number(poll.totalVotes || 0);
  const options = poll.options || [];
  return <Link to={`/polls/${poll.id}`} className="recent-poll-card">
    <div className="recent-card-top"><span className={`status-badge status-${poll.status}`}>{poll.status}</span><span className="recent-card-time">{timeAgo(poll.createdAt)}</span></div>
    <h3>{poll.question}</h3>
    <div className="recent-options">{options.map((option, index) => {
      const percent = total ? Math.round(Number(option.votes || 0) / total * 100) : 0;
      return <div className="recent-option" key={option.id || option.text}><div><span>{option.text}</span><strong>{percent}%</strong></div><i><b className={`data-bar color-${index % 6}`} style={{ width: `${percent}%` }}/></i></div>;
    })}</div>
    <div className="recent-card-footer"><span>{total.toLocaleString()} {total === 1 ? 'vote' : 'votes'}</span><span>{Number(poll.views || 0).toLocaleString()} views</span></div>
  </Link>;
}

export default function DashboardPage() {
  const [data, setData] = useState(empty);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const { user } = useAuth();
  const load = useCallback(async () => {
    try {
      const [overviewRes, seriesRes, topRes, pollsRes] = await Promise.all([
        apiRequest('/api/analytics/overview'), apiRequest('/api/analytics/votes-over-time?days=7'),
        apiRequest('/api/analytics/top-polls'), apiRequest('/api/polls?page=1&limit=3&sort=newest'),
      ]);
      const responses = [overviewRes, seriesRes, topRes, pollsRes];
      const payloads = await Promise.all(responses.map((response) => response.json().catch(() => ({}))));
      const failed = responses.findIndex((response) => !response.ok);
      if (failed >= 0) throw new Error(payloads[failed].error || 'Unable to load dashboard data.');
      setData({ overview: payloads[0], series: payloads[1].series || [], top: payloads[2].polls || [], recent: payloads[3].polls || [] });
      setError('');
    } catch (loadError) { setError(loadError.message || 'Unable to load dashboard.'); }
    finally { setLoading(false); }
  }, []);
  const liveStatus = useDashboardLive(load);
  useEffect(() => { load(); }, [load]);
  const overview = data.overview;
  const stats = [
    ['Total polls', overview?.totalPolls, 'polls'], ['Total votes', overview?.totalVotes, 'votes'],
    ['Total views', overview?.totalViews, 'views'], ['Engagement', overview?.engagement, 'engagement'],
  ];
  const maxTopVotes = Math.max(1, ...data.top.map((poll) => Number(poll.totalVotes || 0)));

  return <div className="owner-app"><OwnerNav/><main className="owner-main dashboard-main">
    <header className="page-heading"><div><h1>Welcome back, {displayName(user?.email)}</h1><p>Here's what's happening with your polls.</p></div><Link className="button button-primary page-create-button" to="/create"><span aria-hidden="true">+</span> Create poll</Link></header>
    {error && <div className="inline-error" role="alert"><span>{error}</span><button className="button button-secondary" type="button" onClick={() => { setLoading(true); load(); }}>Retry</button></div>}
    {loading ? <div className="dashboard-stats" aria-label="Loading dashboard">{stats.map(([label]) => <article className="dashboard-stat skeleton-card" key={label}><span className="skeleton-line"/><span className="skeleton-number"/><span className="skeleton-line short"/></article>)}</div> : <>
      <section className="dashboard-stats" aria-label="Poll statistics">{stats.map(([label, stat, icon]) => <article className="dashboard-stat" key={label}><div className="stat-top"><span>{label}</span><span className={`stat-icon stat-icon-${icon}`} aria-hidden="true"><svg viewBox="0 0 24 24">{statIcons[icon]}</svg></span></div><strong>{icon === 'engagement' ? `${Math.round(Number(stat?.value || 0))}%` : Number(stat?.value || 0).toLocaleString()}</strong><div className="stat-growth"><Growth stat={stat}/><span>vs previous 30 days</span></div></article>)}</section>
      <Link className="dashboard-my-polls" to="/polls"><span className="shortcut-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 13h8M8 17h5"/></svg></span><span className="shortcut-copy"><strong>My polls</strong><span>Manage your questions and keep the conversation moving.</span></span><strong className="shortcut-count">{Number(overview?.totalPolls?.value || 0).toLocaleString()} <small>total</small></strong><span className="shortcut-arrow" aria-hidden="true">›</span></Link>
      <div className="dashboard-columns"><section className="dashboard-card chart-card"><div className="section-heading"><div><h2>Votes over time</h2><p>Your voting activity over the last 7 days.</p></div><span className={`live-badge live-${liveStatus}`}><i/> {liveStatus === 'live' ? 'Live' : liveStatus === 'reconnecting' ? 'Reconnecting' : 'Connecting'}</span></div><AnalyticsLineChart data={data.series} height={280}/></section>
        <section className="dashboard-card top-polls-card"><div className="section-heading"><div><h2>Top performing polls</h2><p>Polls with the most responses.</p></div><Link className="text-link" to="/polls">View all</Link></div>{data.top.length ? <ol className="top-poll-list">{data.top.slice(0, 5).map((poll, index) => <li key={poll.id}><Link to={`/polls/${poll.id}`}><span className="top-poll-rank">{String(index + 1).padStart(2, '0')}</span><span className="top-poll-content"><span className="top-poll-title">{poll.question}</span><i><b style={{ width: `${Math.round(Number(poll.totalVotes || 0) / maxTopVotes * 100)}%` }}/></i></span><strong>{Number(poll.totalVotes || 0).toLocaleString()}</strong></Link></li>)}</ol> : <div className="compact-empty"><span aria-hidden="true">▤</span><strong>No polls yet</strong><p>Create a poll to see it here.</p></div>}</section></div>
      <section className="dashboard-card recent-section"><div className="section-heading"><div><h2>Recent polls</h2><p>Your latest questions and activity.</p></div><Link className="text-link" to="/polls">View all</Link></div>{data.recent.length ? <div className="recent-poll-grid">{data.recent.slice(0, 3).map((poll) => <PollPreviewCard key={poll.id} poll={poll}/>)}</div> : <div className="compact-empty"><span aria-hidden="true">▤</span><strong>No polls found</strong><p>Create your first poll to get started.</p><Link className="button button-secondary" to="/create">Create a poll</Link></div>}</section>
    </>}
  </main><CreatePollFab/></div>;
}
