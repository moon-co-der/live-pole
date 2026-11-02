import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import QRCode from '../components/QRCode.jsx';
import OwnerNav from '../components/OwnerNav.jsx';

const initialOptions = ['', ''];

function toLocalDateTimeValue(date) {
  const localDate = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return localDate.toISOString().slice(0, 16);
}

function localDateValue(date) {
  return toLocalDateTimeValue(date).slice(0, 10);
}

function formatLocalEndTime(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return '';
  const dateParts = new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'long', year: 'numeric' }).formatToParts(date);
  const part = (type) => dateParts.find((item) => item.type === type)?.value;
  const parts = `${part('weekday')}, ${part('day')} ${part('month')} ${part('year')}`;
  const time = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(date);
  const zone = new Intl.DateTimeFormat(undefined, { timeZoneName: 'short' }).formatToParts(date).find((part) => part.type === 'timeZoneName')?.value;
  return `Closes ${parts} at ${time}${zone ? ` ${zone}` : ''}`;
}

export default function CreatePollPage() {
  const navigate = useNavigate();
  const [question, setQuestion] = useState('');
  const [options, setOptions] = useState(initialOptions);
  const [allowMultiple, setAllowMultiple] = useState(false);
  const [endsAt, setEndsAt] = useState('');
  const [endTimePreset, setEndTimePreset] = useState('none');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState(null);
  const [copied, setCopied] = useState(false);
  const now = Date.now();
  const endsAtTimestamp = endsAt ? new Date(endsAt).getTime() : NaN;
  const customEndTimeInvalid = endTimePreset === 'custom' && (!Number.isFinite(endsAtTimestamp) || endsAtTimestamp <= now);
  const today = localDateValue(new Date());

  function updateOption(index, value) {
    setOptions((current) => current.map((option, i) => i === index ? value : option));
  }

  function selectEndTime(preset) {
    if (preset !== 'custom' && endTimePreset === preset) {
      setEndTimePreset('none');
      setEndsAt('');
      return;
    }
    setEndTimePreset(preset);
    if (preset === 'custom') {
      const tomorrowAtNine = new Date();
      tomorrowAtNine.setDate(tomorrowAtNine.getDate() + 1);
      tomorrowAtNine.setHours(9, 0, 0, 0);
      setEndsAt(toLocalDateTimeValue(tomorrowAtNine));
      return;
    }
    if (preset === 'none') {
      if (preset === 'none') setEndsAt('');
      return;
    }
    const durations = { hour: 60 * 60_000, day: 24 * 60 * 60_000, week: 7 * 24 * 60 * 60_000 };
    setEndsAt(toLocalDateTimeValue(new Date(Date.now() + durations[preset])));
  }

  function validate() {
    const cleanQuestion = question.trim();
    if (cleanQuestion.length < 3 || cleanQuestion.length > 200) return 'Your question must be 3 to 200 characters.';
    if (options.length < 2 || options.length > 6) return 'Add between 2 and 6 options.';
    const cleanOptions = options.map((option) => option.trim());
    if (cleanOptions.some((option) => option.length === 0)) return 'Fill in every option.';
    if (cleanOptions.some((option) => [...option].length > 100)) return 'Each option must be 100 characters or fewer.';
    if (new Set(cleanOptions.map((option) => option.toLocaleLowerCase())).size !== cleanOptions.length) return 'Options must be unique.';
    if (endTimePreset === 'custom' && customEndTimeInvalid) return 'Pick a time after now.';
    if (endsAt && (!Number.isFinite(new Date(endsAt).getTime()) || new Date(endsAt).getTime() <= Date.now())) return 'Choose an end time in the future.';
    return '';
  }

  async function submit(event) {
    event.preventDefault();
    setError('');
    if (!event.currentTarget.reportValidity()) return;
    const validationError = validate();
    if (validationError) { setError(validationError); return; }

    const payload = {
      question: question.trim(),
      options: options.map((option) => option.trim()),
      allowMultiple,
    };
    if (endsAt) payload.endsAt = new Date(endsAt).toISOString();
    setBusy(true);
    try {
      const response = await apiRequest('/api/polls', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(result.error || 'Unable to create this poll.');
      setCreated({ ...result, shareURL: new URL(result.shareLink, window.location.origin).href });
    } catch (submitError) {
      setError(submitError.message || 'Could not connect. Please try again.');
    } finally {
      setBusy(false);
    }
  }

  async function copyLink() {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(created.shareURL);
      } else {
        const input = document.createElement('textarea');
        input.value = created.shareURL;
        input.style.position = 'fixed';
        input.style.opacity = '0';
        document.body.appendChild(input);
        input.select();
        const success = document.execCommand('copy');
        input.remove();
        if (!success) throw new Error('Copy failed');
      }
      setCopied(true);
    } catch {
      setError('Could not copy automatically. Select and copy the link above.');
    }
  }

  if (created) {
    return (
      <div className="owner-app"><OwnerNav/><main className="poll-page">
        <section className="created-card">
          <span className="placeholder-brand">POLL CREATED</span>
          <h1>Your poll is ready to share</h1>
          <p className="poll-muted">Anyone with this link can open your poll.</p>
          <div className="share-link-row">
            <a href={created.shareURL} target="_blank" rel="noreferrer">{created.shareURL}</a>
            <button className="button button-secondary" type="button" onClick={copyLink}>{copied ? 'Copied' : 'Copy link'}</button>
          </div>
          <div className="qr-panel"><QRCode value={created.shareURL} /><span>Scan to open this poll</span></div>
          {error && <p className="form-error" role="alert">{error}</p>}
          <div className="created-actions">
            <button className="button button-primary" type="button" onClick={() => navigate(`/polls/${created.poll.slug}`)}>View analytics</button>
            <button className="button button-secondary" type="button" onClick={() => navigate('/dashboard')}>Back to dashboard</button>
          </div>
        </section>
      </main></div>
    );
  }

  return (
    <div className="owner-app"><OwnerNav/><main className="poll-page">
      <section className="poll-create-layout">
        <form className="poll-form-card" onSubmit={submit} noValidate>
          <button className="back-link" type="button" onClick={() => navigate('/dashboard')}>← Dashboard</button>
          <span className="placeholder-brand">NEW POLL</span>
          <h1>Create a poll</h1>
          <p className="poll-muted">Write a question and give people a few choices.</p>

          <label htmlFor="poll-question">Question</label>
          <textarea id="poll-question" value={question} onChange={(event) => setQuestion(event.target.value)} required minLength={3} maxLength={200} placeholder="What would you like to ask?" rows={3} />
          <div className="form-hint">{question.trim().length}/200 characters</div>

          <div className="option-heading"><label>Options</label><span>{options.length} of 6</span></div>
          <div className="poll-option-inputs">
            {options.map((option, index) => (
              <div className="poll-option-row" key={index}>
                <span className="option-number">{index + 1}</span>
                <input aria-label={`Option ${index + 1}`} value={option} onChange={(event) => updateOption(index, event.target.value)} required maxLength={100} placeholder={`Choice ${index + 1}`} />
                <button type="button" className="remove-option" aria-label={`Remove option ${index + 1}`} disabled={options.length <= 2} onClick={() => setOptions((current) => current.filter((_, i) => i !== index))}>×</button>
              </div>
            ))}
          </div>
          <button type="button" className="add-option" disabled={options.length >= 6} onClick={() => setOptions((current) => [...current, ''])}>+ Add option</button>

          <label>End time <span className="optional-label">Optional</span></label>
          <div className="end-time-presets" role="group" aria-label="Poll end time">
            <button type="button" className={endTimePreset === 'hour' ? 'is-selected' : ''} aria-pressed={endTimePreset === 'hour'} onClick={() => selectEndTime('hour')}>1 hour</button>
            <button type="button" className={endTimePreset === 'day' ? 'is-selected' : ''} aria-pressed={endTimePreset === 'day'} onClick={() => selectEndTime('day')}>24 hours</button>
            <button type="button" className={endTimePreset === 'week' ? 'is-selected' : ''} aria-pressed={endTimePreset === 'week'} onClick={() => selectEndTime('week')}>7 days</button>
            <button type="button" className={endTimePreset === 'custom' ? 'is-selected' : ''} aria-pressed={endTimePreset === 'custom'} onClick={() => selectEndTime('custom')}>Custom</button>
          </div>
          {endTimePreset === 'custom' && (
            <div className="end-time-custom-wrap">
              <div className="end-time-custom">
                <div className="end-time-field">
                  <label htmlFor="poll-end-date">Date</label>
                  <input id="poll-end-date" type="date" min={today} value={endsAt.split('T')[0] || ''} onChange={(event) => setEndsAt(event.target.value ? `${event.target.value}T${endsAt.split('T')[1] || '09:00'}` : '')} />
                </div>
                <div className="end-time-field">
                  <label htmlFor="poll-end-time">Time</label>
                  <input id="poll-end-time" type="time" value={endsAt.split('T')[1] || ''} onChange={(event) => setEndsAt(endsAt.split('T')[0] ? `${endsAt.split('T')[0]}T${event.target.value || '09:00'}` : '')} />
                </div>
              </div>
              {customEndTimeInvalid ? <p className="end-time-error" role="alert">Pick a time after now.</p> : <p className="end-time-summary">{formatLocalEndTime(endsAt)}</p>}
              <p className="end-time-help">Voting stops automatically at this time.</p>
            </div>
          )}
          <label className="toggle-row" htmlFor="allow-multiple">
            <span><strong>Allow multiple selections</strong><small>Voters may choose more than one option.</small></span>
            <input id="allow-multiple" type="checkbox" checked={allowMultiple} onChange={(event) => setAllowMultiple(event.target.checked)} />
          </label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="button button-primary create-button" type="submit" disabled={busy}>{busy && <i className="button-spinner" aria-hidden="true"/>}{busy ? 'Creating…' : 'Create poll'}</button>
        </form>

      </section>
    </main></div>
  );
}
