import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiRequest } from '../api/client.js';
import QRCode from '../components/QRCode.jsx';
import OwnerNav from '../components/OwnerNav.jsx';

const initialOptions = ['', ''];

export default function CreatePollPage() {
  const navigate = useNavigate();
  const [question, setQuestion] = useState('');
  const [options, setOptions] = useState(initialOptions);
  const [allowMultiple, setAllowMultiple] = useState(false);
  const [endsAt, setEndsAt] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState(null);
  const [copied, setCopied] = useState(false);

  function updateOption(index, value) {
    setOptions((current) => current.map((option, i) => i === index ? value : option));
  }

  function validate() {
    const cleanQuestion = question.trim();
    if (cleanQuestion.length < 3 || cleanQuestion.length > 200) return 'Your question must be 3 to 200 characters.';
    if (options.length < 2 || options.length > 6) return 'Add between 2 and 6 options.';
    const cleanOptions = options.map((option) => option.trim());
    if (cleanOptions.some((option) => option.length === 0)) return 'Fill in every option.';
    if (cleanOptions.some((option) => [...option].length > 100)) return 'Each option must be 100 characters or fewer.';
    if (new Set(cleanOptions.map((option) => option.toLocaleLowerCase())).size !== cleanOptions.length) return 'Options must be unique.';
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
            <button className="button button-primary" type="button" onClick={() => navigate(`/polls/${created.id}`)}>View analytics</button>
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

          <label htmlFor="poll-ends-at">End time <span className="optional-label">Optional</span></label>
          <input id="poll-ends-at" type="datetime-local" value={endsAt} onChange={(event) => setEndsAt(event.target.value)} />
          <label className="toggle-row" htmlFor="allow-multiple">
            <span><strong>Allow multiple selections</strong><small>Voters may choose more than one option.</small></span>
            <input id="allow-multiple" type="checkbox" checked={allowMultiple} onChange={(event) => setAllowMultiple(event.target.checked)} />
          </label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="button button-primary create-button" type="submit" disabled={busy}>{busy && <i className="button-spinner" aria-hidden="true"/>}{busy ? 'Creating…' : 'Create poll'}</button>
        </form>

        <details className="poll-preview">
          <summary className="preview-label">Poll preview <span aria-hidden="true">⌄</span></summary>
          <div className="preview-content" aria-label="Poll preview">
          <span className="preview-live-label">LIVE PREVIEW</span>
          <div className="preview-card">
            <span className="preview-status"><i /> ACTIVE POLL</span>
            <h2>{question.trim() || 'Your question will appear here'}</h2>
            <div className="preview-options">
              {options.map((option, index) => <div key={index}><span className="preview-radio" />{option.trim() || `Choice ${index + 1}`}</div>)}
            </div>
            <p>{allowMultiple ? 'Choose all that apply' : 'Choose one option'}</p>
          </div>
          </div>
        </details>
      </section>
    </main></div>
  );
}
