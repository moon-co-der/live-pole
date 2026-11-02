import { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import useLivePoll from '../hooks/useLivePoll.js';
import BrandMark from '../components/BrandMark.jsx';

export default function PublicPollPage() {
  const { slug } = useParams();
  const [poll, setPoll] = useState(null);
  const [results, setResults] = useState(null);
  const [selectedOption, setSelectedOption] = useState('');
  const [hasVoted, setHasVoted] = useState(false);
  const [recentVoteTimes, setRecentVoteTimes] = useState([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const live = useLivePoll(slug, Boolean(poll));
  const lastObservedTotal = useRef(null);

  useEffect(() => { lastObservedTotal.current = null; setRecentVoteTimes([]); }, [slug]);
  useEffect(() => {
    const timer = window.setInterval(() => {
      const cutoff = Date.now() - 60 * 60 * 1000;
      setRecentVoteTimes((current) => current.filter((at) => at >= cutoff));
    }, 60_000);
    return () => window.clearInterval(timer);
  }, []);

  const applyResults = useCallback((next) => {
    if (!next) return;
    const total = Number(next.totalVotes || 0);
    const previous = lastObservedTotal.current;
    if (previous !== null && total > previous) {
      const added = Math.min(total - previous, 500);
      const now = Date.now();
      setRecentVoteTimes((current) => [...current.filter((at) => now - at < 60 * 60 * 1000), ...Array(added).fill(now)]);
    }
    lastObservedTotal.current = total;
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
  const leadingSummary = totalVotes === 0
    ? 'Be the first to vote'
    : leaders.length !== 1
      ? 'Tie'
      : `${leaders[0].text} · ${Math.round(highVoteCount / totalVotes * 100)}%`;
  const votesLastHour = recentVoteTimes.filter((at) => Date.now() - at < 60 * 60 * 1000).length;
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
      {!loading && poll && (
        <div className="public-poll-stats" aria-label="Poll activity" aria-live="polite">
          <article className="public-poll-stat"><strong key={`votes-${totalVotes}`} className="public-stat-value">{totalVotes.toLocaleString()}</strong><span>Total votes</span></article>
          <article className={`public-poll-stat${totalVotes === 0 ? ' is-empty' : ''}`}><strong key={`leader-${leadingSummary}`} className="public-stat-value">{leadingSummary}</strong><span>Leading option</span></article>
          <article className="public-poll-stat" title="New votes observed through live updates on this page during the last hour"><strong key={`recent-${votesLastHour}`} className="public-stat-value">{votesLastHour.toLocaleString()}</strong><span>Votes · last hour</span></article>
        </div>
      )}
      <section className="public-poll-card" aria-live="polite">
        <BrandMark/>
        {loading && <div className="vote-skeleton" aria-label="Loading poll"><i/><i/><i/></div>}
        {!loading && error && !poll && <div className="vote-load-error"><p className="public-poll-error" role="alert">{error}</p><button className="button button-secondary" type="button" onClick={loadPoll}>Retry</button></div>}
        {!loading && poll && (
          <>
            <div className="public-poll-status"><i className={statusClass} />{statusLabel}</div>
            <h1>{poll.question}</h1>
            {error && <p className="public-poll-error" role="alert">{error}</p>}
            {hasVoted && liveResults ? (
              <div className="public-poll-results" aria-label="Poll results">
                {liveResults.options.map((option) => {
                  const optionVotes = Number(option.votes) || 0;
                  const percentage = totalVotes > 0 ? Math.round((optionVotes / totalVotes) * 100) : 0;
                  const leading = optionVotes > 0 && optionVotes === highVoteCount && leaders.length === 1;
                  return (
                    <div className={`public-result-row${leading ? ' is-leading' : ''}`} key={option.id}>
                      <div className="public-result-heading"><span>{option.text}{leading && <b className="public-leading-label">Leading</b>}</span><strong>{optionVotes.toLocaleString()} · {percentage}%</strong></div>
                      <div className="public-result-track"><i style={{ width: `${percentage}%` }} /></div>
                    </div>
                  );
                })}
                <p>{liveResults.totalVotes} {liveResults.totalVotes === 1 ? 'vote' : 'votes'}</p>
              </div>
            ) : effectiveClosed ? (
              <p className="public-poll-message">This poll is closed and no longer accepting votes.</p>
            ) : (
              <>
                <form className="public-vote-form" onSubmit={submitVote}>
                  <fieldset disabled={submitting}>
                    <legend>Choose one option</legend>
                    {poll.options.map((option) => (
                      <label className={`public-vote-option${selectedOption === option.id ? ' is-selected' : ''}`} key={option.id}>
                        <input type="radio" name="poll-option" value={option.id} checked={selectedOption === option.id} onChange={() => setSelectedOption(option.id)} />
                        <span>{option.text}</span>
                      </label>
                    ))}
                  </fieldset>
                  <button className="button button-primary" type="submit" disabled={!selectedOption || submitting}>{submitting && <i className="button-spinner" aria-hidden="true"/>}{submitting ? 'Submitting…' : 'Vote'}</button>
                </form>
              </>
            )}
          </>
        )}
      </section>
    </main>
  );
}
