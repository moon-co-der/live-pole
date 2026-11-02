import { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import useLivePoll from '../hooks/useLivePoll.js';
import useRelativeTimeClock from '../hooks/useRelativeTimeClock.js';
import BrandMark from '../components/BrandMark.jsx';
import formatRelativeTime from '../utils/formatRelativeTime.js';

export default function PublicPollPage() {
  const { slug } = useParams();
  const [poll, setPoll] = useState(null);
  const [results, setResults] = useState(null);
  const [selectedOption, setSelectedOption] = useState('');
  const [hasVoted, setHasVoted] = useState(false);
  const [showResults, setShowResults] = useState(false);
  const [lastVoteAt, setLastVoteAt] = useState(null);
  const clock = useRelativeTimeClock();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const live = useLivePoll(slug, Boolean(poll));
  const lastObservedTotal = useRef(null);
  const applyResults = useCallback((next) => {
    if (!next) return;
    const nextTotal = Number(next.totalVotes) || 0;
    if (lastObservedTotal.current !== null && nextTotal > lastObservedTotal.current) setLastVoteAt(Date.now());
    lastObservedTotal.current = nextTotal;
    setResults(next);
  }, []);

  const loadPoll = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const response = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}`, { headers: { 'X-Poll-Referrer': document.referrer } });
      const payload = await response.json().catch(() => ({}));
      if (response.status === 404) throw new Error('This poll could not be found. Check the link and try again.');
      if (!response.ok) throw new Error(payload.error || 'Unable to load this poll right now.');
      setPoll(payload.poll);
      setHasVoted(Boolean(payload.hasVoted));
      setSelectedOption(localStorage.getItem(`livepoll_vote_${slug}`) || '');
      if (payload.results) applyResults(payload.results);
      else {
        try {
          const resultResponse = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}/results`);
          const resultPayload = await resultResponse.json().catch(() => ({}));
          if (resultResponse.ok) applyResults(resultPayload.results);
        } catch { /* The live connection or polling fallback will resync results. */ }
      }
    } catch (loadError) {
      setError(loadError.message || 'Unable to connect. Try again.');
    } finally {
      setLoading(false);
    }
  }, [slug, applyResults]);

  useEffect(() => { loadPoll(); }, [loadPoll]);
  useEffect(() => {
    lastObservedTotal.current = null;
    setLastVoteAt(null);
    setShowResults(false);
    setSelectedOption('');
  }, [slug]);
  const hasLiveSnapshot = live.status === 'live' && live.options.length > 0;
  useEffect(() => {
    if (!poll || !live.options.length) return;
    applyResults({
      options: poll.options.map((option) => ({ ...option, votes: Number(live.counts[option.id] || 0) })),
      totalVotes: Number(live.total || 0),
    });
  }, [applyResults, live.counts, live.options.length, live.total, poll]);

  useEffect(() => {
    if (!poll || hasLiveSnapshot) return undefined;
    let active = true;
    const refresh = async () => {
      try {
        const response = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}/results`);
        const payload = await response.json().catch(() => ({}));
        if (active && response.ok) applyResults(payload.results);
      } catch { /* The next interval retries while the live socket reconnects. */ }
    };
    refresh();
    const timer = window.setInterval(refresh, 4000);
    return () => { active = false; window.clearInterval(timer); };
  }, [applyResults, hasLiveSnapshot, poll, slug]);

  async function submitVote(event) {
    event.preventDefault();
    if (!selectedOption || submitting) return;
    setSubmitting(true);
    setError('');
    try {
      const response = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}/vote`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Poll-Referrer': document.referrer },
        body: JSON.stringify({ optionId: selectedOption }),
      });
      const payload = await response.json().catch(() => ({}));
      if (response.status === 409) {
        await loadPoll();
        setError(payload.error || 'You already voted.');
        return;
      }
      if (response.status === 429) throw new Error(payload.error || 'Too many attempts. Please wait a moment and try again.');
      if (response.status === 404) throw new Error('This poll could not be found.');
      if (response.status === 400) throw new Error(payload.error || 'This poll is closed or your choice is invalid.');
      if (!response.ok) throw new Error(payload.error || 'Unable to submit your vote right now.');
      setHasVoted(true);
      setLastVoteAt(Date.now());
      localStorage.setItem(`livepoll_vote_${slug}`, selectedOption);
      applyResults(payload.results);
    } catch (voteError) {
      setError(voteError.message || 'Unable to submit your vote right now.');
    } finally {
      setSubmitting(false);
    }
  }

  const closed = poll && poll.status !== 'active';
  const effectiveClosed = closed || live.status === 'closed';
  const liveResults = poll && (hasLiveSnapshot ? {
    options: poll.options.map((option) => ({ ...option, votes: live.counts[option.id] ?? 0 })),
    totalVotes: live.total,
  } : results);
  const totalVotes = Number(liveResults?.totalVotes || 0);
  const highVoteCount = liveResults?.options.length ? Math.max(0, ...liveResults.options.map((option) => Number(option.votes) || 0)) : 0;
  const leaders = liveResults?.options.filter((option) => Number(option.votes || 0) === highVoteCount) || [];
  const leadingOption = totalVotes > 0 && leaders.length === 1 ? leaders[0] : null;
  const leadingLabel = totalVotes === 0 ? '—' : leadingOption ? leadingOption.text : 'Tie';
  const leadingPercentage = leadingOption ? `${Math.round(highVoteCount / totalVotes * 100)}%` : null;
  const lastVoteLabel = lastVoteAt === null || totalVotes === 0 ? '—' : formatRelativeTime(lastVoteAt, clock);
  const showingResults = hasVoted || showResults;
  const statusLabel = effectiveClosed
    ? 'POLL CLOSED'
    : hasVoted && live.status === 'reconnecting'
      ? 'RECONNECTING'
      : hasVoted && live.status === 'connecting'
        ? 'CONNECTING'
        : 'LIVE POLL';
  const statusClass = effectiveClosed ? 'is-closed' : hasVoted && ['reconnecting', 'connecting'].includes(live.status) ? 'is-reconnecting' : '';

  return (
    <main className="public-poll-page">
      <header className="public-poll-header">
        <BrandMark/>
        {poll && <div className="public-poll-status"><i className={statusClass} />{statusLabel}</div>}
      </header>
      {poll && <section className="public-poll-stats" aria-label="Poll activity" aria-live="polite">
        <article className="public-poll-stat"><strong key={`total-${totalVotes}`} className="public-stat-value">{totalVotes.toLocaleString()}</strong><span>Total votes</span></article>
        <article className="public-poll-stat"><div key={`leading-${leadingLabel}-${leadingPercentage || ''}`} className="public-leading-stat-value public-stat-value"><span className="leading-option-name">{leadingLabel}</span>{leadingPercentage && <span className="leading-option-percent">{leadingPercentage}</span>}</div><span>Leading</span></article>
        <article className="public-poll-stat"><strong key={`last-${lastVoteLabel}`} className="public-stat-value">{lastVoteLabel}</strong><span>Last vote</span></article>
      </section>}
      <section className="public-poll-card" aria-live="polite">
        {loading && <div className="vote-skeleton" aria-label="Loading poll"><i/><i/><i/></div>}
        {!loading && error && !poll && <div className="vote-load-error"><p className="public-poll-error" role="alert">{error}</p><button className="button button-secondary" type="button" onClick={loadPoll}>Retry</button></div>}
        {!loading && poll && <>
          <h1 className="public-poll-question">{poll.question}</h1>
          {error && <p className="public-poll-error" role="alert">{error}</p>}
          {showingResults ? (
            <div className="public-poll-results" aria-label="Poll results">
              {totalVotes === 0 && <p className="public-poll-message">Be the first to vote</p>}
              {liveResults?.options.map((option) => {
                const optionVotes = Number(option.votes) || 0;
                const percentage = totalVotes > 0 ? Math.round((optionVotes / totalVotes) * 100) : null;
                const leading = optionVotes > 0 && optionVotes === highVoteCount && leaders.length === 1;
                return <div className={`public-result-row${leading ? ' is-leading' : ''}`} key={option.id}>
                  <div className="public-result-heading">
                    <span>{option.text}{leading && <b className="public-leading-label">Leading</b>}{hasVoted && selectedOption === option.id && <b className="public-your-vote-label">Your vote</b>}</span>
                    <div className="public-result-values"><strong>{totalVotes > 0 ? `${percentage}%` : '—'}</strong><small>{optionVotes.toLocaleString()} {optionVotes === 1 ? 'vote' : 'votes'}</small></div>
                  </div>
                  {totalVotes > 0 && <div className="public-result-track"><i style={{ width: `${percentage}%` }} /></div>}
                </div>;
              })}
              <p>{totalVotes.toLocaleString()} {totalVotes === 1 ? 'vote' : 'votes'} total</p>
              {!hasVoted && !effectiveClosed && <button className="public-view-vote" type="button" onClick={() => setShowResults(false)}>Back to voting</button>}
            </div>
          ) : !effectiveClosed ? (
            <form className="public-vote-form" onSubmit={submitVote}>
              <fieldset disabled={submitting}>
                <legend>Choose one option</legend>
                {poll.options.map((option) => <label className={`public-vote-option${selectedOption === option.id ? ' is-selected' : ''}`} key={option.id}>
                  <input type="radio" name="poll-option" value={option.id} checked={selectedOption === option.id} onChange={() => setSelectedOption(option.id)} />
                  <span>{option.text}</span>
                </label>)}
              </fieldset>
              <button className="button button-primary" type="submit" disabled={!selectedOption || submitting}>{submitting && <i className="button-spinner" aria-hidden="true"/>}{submitting ? 'Submitting…' : 'Vote'}</button>
              {liveResults && <button className="public-view-results" type="button" onClick={() => setShowResults(true)}>View results</button>}
            </form>
          ) : <div className="public-poll-message">This poll is closed and no longer accepting votes.{liveResults && <button className="public-view-results" type="button" onClick={() => setShowResults(true)}>View results</button>}</div>}
        </>}
      </section>
    </main>
  );
}
