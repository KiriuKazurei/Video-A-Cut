import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    // Ant Design 5 及其 rc-* 依赖单独成块：应用代码改动时浏览器可继续复用组件库缓存。
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          if (/[\\/]node_modules[\\/](react|react-dom|scheduler|@tanstack)[\\/]/.test(id)) return 'vendor-react';
          return 'vendor-antd';
        }
      }
    }
  },
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
