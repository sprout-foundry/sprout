import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import Layout from './layouts/Layout';
import About from './pages/About';
import Home from './pages/Home';
import Items from './pages/Items';

// The layout wraps every route, so the nav and shell render once around
// whichever page matches. Add a route by adding an entry here and a
// component under src/pages/.
const router = createBrowserRouter([
  {
    path: '/',
    element: <Layout />,
    children: [
      { index: true, element: <Home /> },
      { path: 'about', element: <About /> },
      { path: 'items', element: <Items /> },
    ],
  },
]);

export default function App() {
  return <RouterProvider router={router} />;
}
