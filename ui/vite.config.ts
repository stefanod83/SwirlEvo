import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

const config = loadEnv('development', './')

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    }
  },
  build: {
    cssCodeSplit: false,
  },
  server: {
    port: 3002,
    proxy: {
      '/api': {
        target: 'http://' + config.VITE_PROXY_URL,
        changeOrigin: true,
      },
    }
  },
})
