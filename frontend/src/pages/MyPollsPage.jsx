import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import OwnerNav from '../components/OwnerNav.jsx';
import CreatePollFab from '../components/CreatePollFab.jsx';
import ConfirmDialog from '../components/ConfirmDialog.jsx';

const tabs = [{ label: 'All', value: '' }, { label: 'Active', value: 'active' }, { label: 'Completed', value: 'completed' }, { label: 'Drafts', value: 'draft' }];

function engagement(poll) {
  const views = Number(poll.views || 0);
  return views ? Math.round(Number(poll.totalVotes || 0) / views * 100) : 0;
}

function PollActionMenu({ poll, open, onToggle, onChoose, triggerRef }) {
  return <div className={`poll-row-menu${open ? ' is-open' : ''}`}>
    <button ref={triggerRef} className="icon-button poll-menu-trigger" type="button" aria-label={`Actions for ${poll.question}`} aria-haspopup="menu" aria-expanded={open} onClick={onToggle}><span aria-hidden="true">•••</span></button>
    {open && <><button className="menu-backdrop" type="button" aria-label="Close actions" onClick={onToggle}/><div className="poll-menu-popover" role="menu">
      <Link role="menuitem" to={`/polls/${poll.id}`} onClick={onToggle}>View analytics</Link>
      <button role="menuitem" type="button" onClick={() => onChoose('duplicate', poll)}>Duplicate</button>
      {poll.status === 'active' && <button role="menuitem" type="button" onClick={() => onChoose('close', poll)}>Close poll</button>}
      <button role="menuitem" className="menu-danger" type="button" onClick={() => onChoose('delete', poll)}>Delete poll</button>
    </div></>}
  </div>;
}

export default function MyPollsPage() {
  const [status, setStatus] = useState('');
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState('newest');
  const [polls, setPolls] = useState([]);
  const [error, setError] = useState('');
  const [toast, setToast] = useState('');
  const [loading, setLoading] = useState(true);
  const [menuPoll, setMenuPoll] = useState(null);
  const [pendingAction, setPendingAction] = useState(null);
  const menuRef = useRef(null);
  const menuTriggerRef = useRef(null);
  const navigate = useNavigate();
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const query = new URLSearchParams({ page: '1', limit: '100', sort });
      if (status) query.set('status', status);
      if (search.trim()) query.set('q', search.trim());
      const response = await apiRequest(`/api/polls?${query}`);
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || 'Unable to load your polls.');
      setPolls(payload.polls || []); setError('');
    } catch (loadError) { setError(loadError.message || 'Unable to load your polls.'); }
    finally { setLoading(false); }
  }, [status, search, sort]);
  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    if (!menuPoll) return undefined;
    function closeOutside(event) { if (!menuRef.current?.contains(event.target)) setMenuPoll(null); }
    function closeEscape(event) {
      if (event.key === 'Escape') { setMenuPoll(null); menuTriggerRef.current?.focus(); }
    }
    document.addEventListener('pointerdown', closeOutside);
    document.addEventListener('keydown', closeEscape);
    return () => { document.removeEventListener('pointerdown', closeOutside); document.removeEventListener('keydown', closeEscape); };
  }, [menuPoll]);
  const notify = useCallback((message) => { setToast(message); window.setTimeout(() => setToast(''), 3000); }, []);
  const closeMenu = useCallback(() => setMenuPoll(null), []);
  const cancelConfirmation = useCallback(() => setPendingAction(null), []);

  async function duplicatePoll(poll) {
    const response = await apiRequest('/api/polls', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ question: poll.question, options: poll.options.map((option) => option.text), allowMultiple: poll.allowMultiple }) });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) { setError(payload.error || 'Could not duplicate the poll.'); return; }
    navigate(`/polls/${payload.id}`, { state: { toast: 'Poll duplicated.' } });
  }

  async function confirmAction() {
    if (!pendingAction) return;
    const { type, poll } = pendingAction;
    setPendingAction(null);
    const response = type === 'close'
      ? await apiRequest(`/api/polls/${poll.id}/close`, { method: 'PATCH' })
      : await apiRequest(`/api/polls/${poll.id}`, { method: 'DELETE' });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) { setError(payload.error || `Could not ${type} the poll.`); return; }
    notify(type === 'close' ? 'Poll closed.' : 'Poll deleted.');
    load();
  }

  function chooseAction(type, poll) {
    setMenuPoll(null);
    if (type === 'duplicate') { duplicatePoll(poll); return; }
    setPendingAction({ type, poll });
  }

  return <div className="owner-app"><OwnerNav/><main className="owner-main">
    <header className="page-heading"><div><h1>My polls</h1><p>Find, manage, and review your polls.</p></div><Link className="button button-primary page-create-button" to="/create"><span aria-hidden="true">+</span> Create poll</Link></header>
    {error && <div className="inline-error" role="alert"><span>{error}</span><button className="button button-secondary" type="button" onClick={load}>Retry</button></div>}
    <section className="dashboard-card polls-card" aria-label="Your polls">
      <div className="polls-toolbar">
        <div className="poll-filters"><label className="poll-search"><span aria-hidden="true">⌕</span><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search polls" aria-label="Search polls"/></label><label className="sort-select"><span className="sr-only">Sort polls</span><select value={sort} onChange={(event) => setSort(event.target.value)} aria-label="Sort polls"><option value="newest">Newest</option><option value="votes">Most votes</option></select></label></div>
        <div className="poll-tabs" role="group" aria-label="Filter polls">{tabs.map((tab) => <button type="button" aria-pressed={status === tab.value} className={status === tab.value ? 'is-selected' : ''} onClick={() => setStatus(tab.value)} key={tab.label}>{tab.label}</button>)}</div>
      </div>
      {loading ? <div className="poll-list-skeleton" aria-label="Loading polls">{Array.from({ length: 3 }, (_, index) => <div className="skeleton-row" key={index}><i/><span/><b/></div>)}</div> : polls.length ? <>
        <div className="poll-table-head" aria-hidden="true"><span>Poll</span><span>Status</span><span>Votes</span><span>Engagement</span><span>Created</span><span/></div>
        <div className="poll-table">{polls.map((poll) => <article className="poll-table-row" key={poll.id}>
          <div className="poll-row-main"><Link to={`/polls/${poll.id}`}><strong>{poll.question}</strong></Link><span className={`status-badge status-${poll.status}`}>{poll.status}</span><small>{new Date(poll.createdAt).toLocaleDateString()}</small></div>
          <span className={`status-badge poll-row-status status-${poll.status}`}>{poll.status}</span>
          <span className="poll-row-value"><strong>{Number(poll.totalVotes || 0).toLocaleString()}</strong><small>votes</small></span>
          <span className="poll-row-engagement"><strong>{engagement(poll)}%</strong><small>of views</small></span>
          <time className="poll-row-time" dateTime={poll.createdAt}>{new Date(poll.createdAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}</time>
          <div ref={menuPoll?.id === poll.id ? menuRef : null}><PollActionMenu poll={poll} open={menuPoll?.id === poll.id} triggerRef={menuPoll?.id === poll.id ? menuTriggerRef : null} onToggle={() => setMenuPoll((current) => current?.id === poll.id ? null : poll)} onChoose={chooseAction}/></div>
        </article>)}</div>
      </> : <div className="empty-state"><span aria-hidden="true">▤</span><h2>No polls found</h2><p>Try another search or create your first poll.</p><Link className="button button-primary" to="/create">Create a poll</Link></div>}
    </section>
  </main><CreatePollFab/>
    {toast && <div className="owner-toast" role="status">{toast}</div>}
    {pendingAction && <ConfirmDialog title={pendingAction.type === 'close' ? 'Close this poll?' : 'Delete this poll?'} message={pendingAction.type === 'close' ? `“${pendingAction.poll.question}” will stop accepting responses.` : `“${pendingAction.poll.question}”, its votes and analytics will be permanently deleted.`} confirmLabel={pendingAction.type === 'close' ? 'Close poll' : 'Delete poll'} danger={pendingAction.type === 'delete'} onCancel={cancelConfirmation} onConfirm={confirmAction}/>}
  </div>;
}
