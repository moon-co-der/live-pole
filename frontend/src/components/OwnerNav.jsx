import { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useAuth } from '../context/AuthContext.jsx';
import BrandMark from './BrandMark.jsx';

export default function OwnerNav() {
  const location = useLocation();
  const { logout, user } = useAuth();
  const [menuOpen, setMenuOpen] = useState(false);
  const headerRef = useRef(null);
  const menuButtonRef = useRef(null);
  const email = user?.email || 'Poll owner';
  const initial = email.charAt(0).toUpperCase();
  const dashboardCurrent = location.pathname === '/dashboard';
  const pollsCurrent = location.pathname === '/polls' || location.pathname.startsWith('/polls/');

  useEffect(() => {
    if (!menuOpen) return undefined;
    const firstItem = headerRef.current?.querySelector('.mobile-menu-panel a, .mobile-menu-panel button');
    firstItem?.focus();
    function closeOutside(event) {
      if (!headerRef.current?.contains(event.target)) setMenuOpen(false);
    }
    function handleKeyDown(event) {
      if (event.key === 'Escape') {
        setMenuOpen(false);
        menuButtonRef.current?.focus();
        return;
      }
      if (event.key !== 'Tab') return;
      const items = headerRef.current?.querySelectorAll('.mobile-menu-panel a, .mobile-menu-panel button');
      if (!items?.length) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }
    document.addEventListener('pointerdown', closeOutside);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', closeOutside);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [menuOpen]);

  function closeMenu() { setMenuOpen(false); }
  function handleLogout() { closeMenu(); logout(); }
  return (
    <header ref={headerRef} className="owner-nav">
      <Link to="/dashboard" className="owner-brand" aria-label="LivePoll dashboard"><BrandMark /></Link>
      <nav className="owner-nav-links" aria-label="Main navigation">
        <Link className={dashboardCurrent ? 'is-current' : ''} aria-current={dashboardCurrent ? 'page' : undefined} to="/dashboard">Dashboard</Link>
        <Link className={pollsCurrent ? 'is-current' : ''} aria-current={pollsCurrent ? 'page' : undefined} to="/polls">My polls</Link>
      </nav>
      <div className="owner-account"><span title={email}>{email}</span><button className="button button-secondary logout-desktop" type="button" onClick={logout}>Log out</button><span className="owner-avatar" aria-label={`Signed in as ${email}`}>{initial}</span></div>
      <button ref={menuButtonRef} className="mobile-menu-button" type="button" aria-label={menuOpen ? 'Close menu' : 'Open menu'} aria-expanded={menuOpen} aria-controls="owner-mobile-menu" onClick={() => setMenuOpen((open) => !open)}>
        <span /><span /><span />
      </button>
      <div id="owner-mobile-menu" className={`mobile-menu-panel${menuOpen ? ' is-open' : ''}`} aria-hidden={!menuOpen}>
        <nav aria-label="Mobile navigation">
          <Link className={dashboardCurrent ? 'is-current' : ''} to="/dashboard" onClick={closeMenu}>Dashboard</Link>
          <Link className={pollsCurrent ? 'is-current' : ''} to="/polls" onClick={closeMenu}>My polls</Link>
        </nav>
        <div className="mobile-menu-account"><span className="owner-avatar" aria-hidden="true">{initial}</span><span title={email}>{email}</span></div>
        <button className="button button-secondary" type="button" onClick={handleLogout}>Log out</button>
      </div>
    </header>
  );
}
