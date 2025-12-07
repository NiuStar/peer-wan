import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';

export default defineConfig({
  plugins: [react()],
  base: './',
  root: resolve(__dirname),
  build: {
    outDir: resolve(__dirname, '../assets/web'),
    emptyOutDir: true,
  },
});
