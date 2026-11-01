import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const proxyTarget = env.VITE_DEV_API_TARGET;
  const proxy = proxyTarget ? {
    '/api': proxyTarget,
    '/ws': { target: proxyTarget.replace(/^http/, 'ws'), ws: true },
  } : undefined;
  return {
    plugins: [react()],
    server: {
      port: 5173,
      ...(proxy ? { proxy } : {}),
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      rollupOptions: {
        output: {
          manualChunks(id) {
            const path = id.replaceAll('\\', '/');
            if (path.includes('/node_modules/react/') || path.includes('/node_modules/react-dom/') || path.includes('/node_modules/react-router/')) return 'react-vendor';
          },
        },
      },
    },
  };
});
