// 构建信息（版本 tag / 提交号 / 构建时间）：由 vite.config.ts 在构建时注入。
// 用于「设置 → 关于」展示，也便于排查用户实际运行的版本。

export const APP_VERSION: string = typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : 'dev'
export const APP_COMMIT: string = typeof __APP_COMMIT__ === 'string' ? __APP_COMMIT__ : ''
export const BUILD_TIME: string = typeof __BUILD_TIME__ === 'string' ? __BUILD_TIME__ : ''

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** 构建日期：2026-09-19。 */
export function buildDateLabel(): string {
  const d = new Date(BUILD_TIME)
  if (Number.isNaN(d.getTime())) return '未知'
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** 构建时间（本地时区）：2026-09-19 10:34:12。 */
export function buildTimeLabel(): string {
  const d = new Date(BUILD_TIME)
  if (Number.isNaN(d.getTime())) return '未知'
  return `${buildDateLabel()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** 平台标识 → 展示名。 */
export function platformLabel(platform: string): string {
  const map: Record<string, string> = {
    darwin: 'macOS',
    windows: 'Windows',
    linux: 'Linux',
  }
  return map[platform] ?? platform ?? '未知'
}

/** 平台兜底：后端平台接口不可用时按 UA 粗略识别。 */
export function detectPlatform(): string {
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
  if (/Macintosh|iPhone|iPad/i.test(ua)) return 'darwin'
  if (/Windows/i.test(ua)) return 'windows'
  if (/Linux|X11/i.test(ua)) return 'linux'
  return ''
}
