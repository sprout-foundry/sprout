import { Link } from 'react-router-dom';

export default function About() {
  return (
    <>
      <h1>About</h1>
      <p>
        This page lives at <code>/about</code>. Add another route by adding an entry in{' '}
        <code>src/App.tsx</code> and a component under <code>src/pages/</code>.
      </p>
      <p>
        <Link to="/">Back home</Link>
      </p>
    </>
  );
}
