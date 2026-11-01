import { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext.jsx';
import BrandMark from '../components/BrandMark.jsx';

export default function LoginPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(event) {
    event.preventDefault();
    setError('');
    const form = event.currentTarget;
    if (!form.reportValidity()) return;
    const data = new FormData(form);
    setBusy(true);
    try {
      await login(String(data.get('email') || '').trim().toLowerCase(), String(data.get('password') || ''));
      navigate('/dashboard', { replace: true });
    } catch (submitError) {
      setError(submitError.message || 'Could not connect. Try again.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="auth-page">
      <section className="auth-visual" aria-label="A cozy workspace with a live poll dashboard">
        <div className="auth-visual-copy">
          <BrandMark/>
          <h2>Good questions bring us <em>together.</em></h2>
          <p>Share your point of view. See what everyone thinks.</p>
        </div>
        <img src="/login-illustration.png" alt="" aria-hidden="true" />
      </section>
      <section className="auth-panel">
        <div className="auth-card" aria-labelledby="login-title">
          <div className="auth-form-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 5.5h16v11H9l-5 3v-14Z" /><path d="M8 9h8M8 13h5" /></svg></div>
          <h1 id="login-title">Welcome back</h1>
          <p className="auth-description">Sign in to pick up where the conversation left off.</p>
          <nav className="auth-tabs" aria-label="Account access"><span aria-current="page">Sign in</span><Link to="/signup">Create account</Link></nav>
          {location.state?.message && <p className="form-success" role="status">{location.state.message}</p>}
          <form onSubmit={submit} noValidate>
            <label htmlFor="login-email">Email address</label>
            <input id="login-email" name="email" type="email" autoComplete="email" required maxLength={254} placeholder="you@example.com" />
            <label htmlFor="login-password">Password</label>
            <input id="login-password" name="password" type="password" autoComplete="current-password" required placeholder="Your password" />
            {error && <p className="form-error" role="alert">{error}</p>}
            <button className="button button-primary auth-submit" type="submit" disabled={busy}>{busy && <i className="button-spinner" aria-hidden="true"/>}{busy ? 'Signing in…' : <>Sign in <span aria-hidden="true">→</span></>}</button>
          </form>
          <p className="auth-switch">New to Livepoll? <Link to="/signup">Create an account</Link></p>
          <p className="auth-secure"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.5 3.8 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/></svg> Your account is private and secure</p>
        </div>
      </section>
    </main>
  );
}
