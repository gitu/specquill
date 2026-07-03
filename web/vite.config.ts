import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Production build lands in the Go server's embed dir; dev proxies the API.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../server/internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8643', ws: true },
      '/auth': 'http://127.0.0.1:8643',
    },
  },
});
