import { Link } from 'react-router-dom';

export default function CreatePollFab() {
  return <Link className="create-poll-fab" to="/create" aria-label="Create poll"><span aria-hidden="true">+</span></Link>;
}
