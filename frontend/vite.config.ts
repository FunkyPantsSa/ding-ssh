import {execFileSync} from 'node:child_process'
import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

/** 读取 git 信息（tag / 提交号）；非 git 环境或命令失败时返回空串。 */
function gitOutput(args: string[]): string {
  try {
    return execFileSync('git', args, {encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore']}).trim()
  } catch {
    return ''
  }
}

const commit = gitOutput(['rev-parse', '--short', 'HEAD'])
// 优先 CI 注入的版本号，其次 git describe（含 tag、领先提交数与 dirty 标记）。
const version = process.env.APP_VERSION?.trim() || gitOutput(['describe', '--tags', '--always', '--dirty']) || 'dev'
const buildTime = new Date().toISOString()

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
    __APP_COMMIT__: JSON.stringify(commit),
    __BUILD_TIME__: JSON.stringify(buildTime),
  },
  optimizeDeps: {
    include: ['zmodem.js'],
  },
  build: {
    commonjsOptions: {
      include: [/zmodem\.js/, /node_modules/],
    },
  },
})
