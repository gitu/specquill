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
    // dedicated port + strictPort: the Go server's dev proxy (:8643 → vite)
    // dials this address; a silent bump to port+1 would strand it. 127.0.0.1
    // pinned so the proxy target matches the bind address (not just ::1).
    host: '127.0.0.1',
    port: 5643,
    strictPort: true,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8643', ws: true },
      '/auth': 'http://127.0.0.1:8643',
    },
  },
});
