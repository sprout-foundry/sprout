import { Link } from 'react-router-dom';
import { useLocalStorage } from '../hooks/useLocalStorage';

export default function Home() {
  const [visits, setVisits] = useLocalStorage('visits', 0);

  return (
    <>
      <h1>Welcome</h1>
      <p>
        This is a minimal React app. Edit <code>src/pages/Home.tsx</code> to change this page.
      </p>
      <p>
        You have opened this page <strong data-testid="visit-count">{visits}</strong> time(s). The
        count is stored in <code>localStorage</code>, so it survives a reload.
      </p>
      <button type="button" onClick={() => setVisits(visits + 1)}>
        Add a visit
      </button>
      <p>
        <Link to="/about">Learn more about this starter</Link>.
      </p>
    </>
  );
}
