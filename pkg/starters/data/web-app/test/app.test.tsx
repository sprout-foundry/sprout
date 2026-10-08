import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import Layout from '../src/layouts/Layout';
import About from '../src/pages/About';
import Home from '../src/pages/Home';

// The tests drive the real router with an in-memory history so any route can
// be rendered directly, without a browser. Route navigation itself is not
// exercised here: react-router's data router navigates through fetch, which
// jsdom cannot drive — each route is asserted by rendering it.
function renderRoute(path: string) {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <Layout />,
        children: [
          { index: true, element: <Home /> },
          { path: 'about', element: <About /> },
        ],
      },
    ],
    { initialEntries: [path] },
  );
  return render(<RouterProvider router={router} />);
}

describe('web app', () => {
  it('renders the home route inside the shared layout', () => {
    renderRoute('/');
    expect(screen.getByRole('heading', { name: 'Welcome' })).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'Primary' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Home' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'About' })).toHaveAttribute('href', '/about');
  });

  it('renders the about route inside the shared layout', () => {
    renderRoute('/about');
    expect(screen.getByRole('heading', { name: 'About' })).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'Primary' })).toBeInTheDocument();
  });

  it('persists client state to localStorage', async () => {
    const user = userEvent.setup();
    renderRoute('/');

    expect(screen.getByTestId('visit-count')).toHaveTextContent('0');
    expect(localStorage.getItem('visits')).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Add a visit' }));

    expect(screen.getByTestId('visit-count')).toHaveTextContent('1');
    expect(localStorage.getItem('visits')).toBe('1');
  });

  it('restores persisted client state on mount', () => {
    localStorage.setItem('visits', '5');
    renderRoute('/');
    expect(screen.getByTestId('visit-count')).toHaveTextContent('5');
  });

  it('falls back to the initial value when stored state is malformed', () => {
    localStorage.setItem('visits', '{not json');
    renderRoute('/');
    expect(screen.getByTestId('visit-count')).toHaveTextContent('0');
  });
});
