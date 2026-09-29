import { defineConfig } from 'vite';

export default defineConfig({
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.VAC_API_TARGET || 'http://127.0.0.1:8787',
        changeOrigin: true,
        configure(proxy) {
          // The browser talks only to Vite. Go sees a local proxy request, not
          // the development page's cross-origin Origin header.
          proxy.on('proxyReq', (request) => request.removeHeader('origin'));
        }
      }
    }
  }
});
