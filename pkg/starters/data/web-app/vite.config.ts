import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Vite builds the client-side app into dist/.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
  },
});
