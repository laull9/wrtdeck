import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

// 开发期后端地址，可通过环境变量覆盖
const backend = process.env.WRTDECK_BACKEND ?? 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  // 相对基址：面板既要能被面板自身的服务挂在根路径（/），
  // 也要能被设备自带的 Web 服务器挂在子目录（/wrtdeck/）。
  // 绝对路径的 /assets/... 在子目录下会指到别的应用上去。
  base: './',
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    // 开发期把 /api 转发到 Go 服务，前端无需处理跨域
    proxy: {
      '/api': {
        target: backend,
        changeOrigin: true,
      },
    },
  },
  build: {
    // 产物直接进入 Go 的 embed 目录
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    target: 'es2020',
    cssCodeSplit: false,
    chunkSizeWarningLimit: 300,
  },
})
