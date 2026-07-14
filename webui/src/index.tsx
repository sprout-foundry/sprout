import { Buffer } from 'buffer';
globalThis.Buffer = globalThis.Buffer || Buffer;
import '@sprout/ui/dist/style.css';
import './bootstrapAdapter'; // Must be first — installs adapter before component tree
import ReactDOM from 'react-dom/client';
import './index.css';
import App from './App';

const root = ReactDOM.createRoot(document.getElementById('root') as HTMLElement);
root.render(<App />);
