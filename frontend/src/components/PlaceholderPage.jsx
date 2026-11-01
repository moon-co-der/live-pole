import { Link, useLocation, useParams } from 'react-router-dom';
import { useAuth } from '../context/AuthContext.jsx';

export default function PlaceholderPage() {
  const location = useLocation();
  const params = useParams();
  const { user, logout } = useAuth();
  const heading = location.pathname
    .split('/')
    .filter(Boolean)
    .map((part) => part.startsWith(':') ? params[part.slice(1)] : part)
    .join(' / ') || 'Home';

  return (
    <main className="placeholder-page">
      <section className="placeholder-card">
        <span className="placeholder-brand">LIVEPOLL</span>
        <h1>{heading}</h1>
        <p>This page is ready for the next module.</p>
        <code>{location.pathname}</code>
        {user && <nav className="owner-nav"><Link to="/create">Create poll</Link><Link to="/polls">My polls</Link><Link to="/profile">Profile</Link></nav>}
        {user && <p className="signed-in-as">Signed in as {user.email} <button className="logout-button" type="button" onClick={logout}>Log out</button></p>}
      </section>
    </main>
  );
}
