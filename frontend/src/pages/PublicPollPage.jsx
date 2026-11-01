import { useCallback, useEffect, useState } from 'react';
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
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const live = useLivePoll(slug);

  const loadPoll = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const response = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}`);
      const payload = await response.json().catch(() => ({}));
      if (response.status === 404) throw new Error('This poll could not be found. Check the link and try again.');
      if (!response.ok) throw new Error(payload.error || 'Unable to load this poll right now.');
      setPoll(payload.poll);
      setHasVoted(Boolean(payload.hasVoted));
      setResults(payload.results || null);
    } catch (loadError) {
      setError(loadError.message || 'Unable to connect. Try again.');
    } finally {
      setLoading(false);
    }
  }, [slug]);

  useEffect(() => { loadPoll(); }, [loadPoll]);

  async function submitVote(event) {
    event.preventDefault();
    if (!selectedOption || submitting) return;
    setSubmitting(true);
    setError('');
    try {
      const response = await apiRequest(`/api/public/polls/${encodeURIComponent(slug)}/vote`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
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
      setResults(payload.results || null);
      setHasVoted(true);
    } catch (voteError) {
      setError(voteError.message || 'Unable to submit your vote right now.');
    } finally {
      setSubmitting(false);
    }
  }

  const closed = poll && poll.status !== 'active';
  const effectiveClosed = closed || live.status === 'closed';
  const liveResults = poll && (live.options.length ? {
    options: poll.options.map((option) => ({ ...option, votes: live.counts[option.id] ?? 0 })),
    totalVotes: live.total,
  } : results);

  return (
    <main className="public-poll-page">
      <section className="public-poll-card" aria-live="polite">
        <BrandMark/>
        {loading && <div className="vote-skeleton" aria-label="Loading poll"><i/><i/><i/></div>}
        {!loading && error && !poll && <div className="vote-load-error"><p className="public-poll-error" role="alert">{error}</p><button className="button button-secondary" type="button" onClick={loadPoll}>Retry</button></div>}
        {!loading && poll && (
          <>
            <div className="public-poll-status"><i className={effectiveClosed ? 'is-closed' : live.status === 'reconnecting' || live.status === 'connecting' ? 'is-reconnecting' : ''} />{effectiveClosed ? 'POLL CLOSED' : live.status === 'reconnecting' || live.status === 'connecting' ? 'RECONNECTING' : hasVoted ? 'VOTE RECORDED · LIVE' : 'LIVE POLL'}</div>
            <h1>{poll.question}</h1>
            {error && <p className="public-poll-error" role="alert">{error}</p>}
            {hasVoted && liveResults ? (
              <div className="public-poll-results" aria-label="Poll results">
                {liveResults.options.map((option) => {
                  const percentage = liveResults.totalVotes ? Math.round((option.votes / liveResults.totalVotes) * 100) : 0;
                  return (
                    <div className="public-result-row" key={option.id}>
                      <div className="public-result-heading"><span>{option.text}</span><strong>{percentage}%</strong></div>
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
                {live.options.length > 0 && <div className="public-poll-results live-preview-results" aria-label="Live poll results">
                  {liveResults.options.map((option) => {
                    const percentage = liveResults.totalVotes ? Math.round((option.votes / liveResults.totalVotes) * 100) : 0;
                    return <div className="public-result-row" key={option.id}><div className="public-result-heading"><span>{option.text}</span><strong>{percentage}%</strong></div><div className="public-result-track"><i style={{ width: `${percentage}%` }} /></div></div>;
                  })}
                  <p>{liveResults.totalVotes} {liveResults.totalVotes === 1 ? 'vote' : 'votes'}</p>
                </div>}
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
            <p className="public-poll-footer">Your response is private.</p>
          </>
        )}
      </section>
    </main>
  );
}
