import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  optimizeDeps: {
    include: ['zmodem.js'],
  },
  build: {
    commonjsOptions: {
      include: [/zmodem\.js/, /node_modules/],
    },
  },
})
