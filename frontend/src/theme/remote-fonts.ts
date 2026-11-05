import {reactive} from 'vue'

export const remoteFontStatus = reactive<Record<string, 'loading' | 'ready' | 'error'>>({})
const pending = new Map<string, Promise<void>>()
const CACHE = 'ding-terminal-fonts-v1'
const SARASA = 'https://cdn.jsdelivr.net/gh/xiaolai/claudepot-app@831df883498b1cecb9644ac1ba6c04c5d907e267/public/fonts/'

async function download(url: string): Promise<Response> {
  let cache: Cache | undefined
  try { cache = await caches.open(CACHE) } catch { /* Private/unsupported storage: use HTTP cache. */ }
  const saved = await cache?.match(url)
  if (saved) return saved
  const response = await fetch(url, {signal: AbortSignal.timeout(30_000)})
  if (!response.ok) throw new Error(`字体下载失败 (${response.status})`)
  try { await cache?.put(url, response.clone()) } catch { /* Storage quota must not prevent font use. */ }
  return response
}

async function addFace(family: string, weight: number, url: string, unicodeRange?: string) {
  const data = await (await download(url)).arrayBuffer()
  const face = new FontFace(family, data, {weight: String(weight), ...(unicodeRange ? {unicodeRange} : {})})
  await face.load()
  const fonts = document.fonts as FontFaceSet & {add(font: FontFace): FontFaceSet}
  fonts.add(face)
}

async function install(family: string, weight: number) {
  if (family === 'Sarasa Mono SC') {
    // Sarasa has no Medium; share Regular between 400 and 500.
    const file = weight === 700 ? 'Bold' : 'Regular'
    await addFace(family, weight, `${SARASA}SarasaMonoSC-${file}.woff2`)
    return
  }
  const variant = weight === 700 ? 'bold' : weight === 500 ? 'medium' : 'main'
  const cssURL = `https://fontsapi.zeoseven.com/442/${variant}/result.css`
  const css = await (await download(cssURL)).text()
  const blocks = [...css.matchAll(/@font-face\s*\{([^}]+)\}/g)]
  const faces = blocks.map(([, block]) => {
    const source = block.match(/url\(["']?([^"')]+)["']?\)/)?.[1]
    const range = block.match(/unicode-range\s*:\s*([^;}]+)/)?.[1]
    if (!source) throw new Error('字体源缺少文件地址')
    return {url: new URL(source, cssURL).href, range}
  })
  if (!faces.length) throw new Error('字体源未返回字体定义')
  // Fetch complete coverage, not only the preview characters: new terminal output
  // must never enter the glyph atlas before its Chinese/icon shard has loaded.
  let next = 0
  await Promise.all(Array.from({length: Math.min(8, faces.length)}, async () => {
    while (next < faces.length) {
      const face = faces[next++]
      await addFace(family, weight, face.url, face.range)
    }
  }))
}

export function ensureRemoteFont(family: string, weight: number): Promise<void> {
  if (family !== 'Maple Mono NF CN' && family !== 'Sarasa Mono SC') return Promise.resolve()
  const key = `${family}:${weight}`
  const existing = pending.get(key)
  if (existing) return existing
  remoteFontStatus[key] = 'loading'
  const loading = install(family, weight).then(() => {
    remoteFontStatus[key] = 'ready'
  }).catch((error: unknown) => {
    remoteFontStatus[key] = 'error'
    pending.delete(key) // A later selection can retry a failed download.
    throw error
  })
  pending.set(key, loading)
  return loading
}
