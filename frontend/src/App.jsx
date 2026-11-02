import { Navigate, Route, Routes } from 'react-router-dom';
import RoutePlaceholder from './pages/RoutePlaceholder.jsx';
import LoginPage from './pages/LoginPage.jsx';
import SignupPage from './pages/SignupPage.jsx';
import CreatePollPage from './pages/CreatePollPage.jsx';
import PublicPollPage from './pages/PublicPollPage.jsx';
import DashboardPage from './pages/DashboardPage.jsx';
import MyPollsPage from './pages/MyPollsPage.jsx';
import PollAnalyticsPage from './pages/PollAnalyticsPage.jsx';
import { AuthProvider, useAuth } from './context/AuthContext.jsx';

function ProtectedRoute({ children }) {
  const { token, user, loading } = useAuth();
  if (loading) return <main className="placeholder-page"><p>Checking your session…</p></main>;
  if (!token || !user) return <Navigate to="/login" replace />;
  return children;
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/dashboard" replace />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/signup" element={<SignupPage />} />
      <Route path="/dashboard" element={<ProtectedRoute><DashboardPage /></ProtectedRoute>} />
      <Route path="/polls" element={<ProtectedRoute><MyPollsPage /></ProtectedRoute>} />
      <Route path="/polls/:slug" element={<ProtectedRoute><PollAnalyticsPage /></ProtectedRoute>} />
      <Route path="/create" element={<ProtectedRoute><CreatePollPage /></ProtectedRoute>} />
      <Route path="/profile" element={<ProtectedRoute><RoutePlaceholder /></ProtectedRoute>} />
      <Route path="/p/:slug" element={<PublicPollPage />} />
      <Route path="*" element={<Navigate to="/login" replace />} />
    </Routes>
  );
}

export default function App() {
  return <AuthProvider><AppRoutes /></AuthProvider>;
}
