import {t} from '../i18n'
import {reactive} from 'vue'
import {isRemoteFont, latinUnicodeRange} from './font-catalog'
import {fontDataCache} from './font-cache'

export const remoteFontStatus = reactive<Record<string, 'loading' | 'ready' | 'error'>>({})
export const remoteFontProgress = reactive<Record<string, {done: number; total: number}>>({})
const pending = new Map<string, Promise<void>>()
const downloads = new Map<string, Promise<ArrayBuffer>>()
const CACHE = 'ding-terminal-fonts-v1'
const SARASA = 'https://cdn.jsdelivr.net/gh/xiaolai/claudepot-app@831df883498b1cecb9644ac1ba6c04c5d907e267/public/fonts/'
const LATIN_SOURCES: Record<string, string> = {
  'JetBrains Mono': 'https://cdn.jsdelivr.net/npm/@fontsource/jetbrains-mono@5.3.0/files/jetbrains-mono-latin-',
  'Fira Code': 'https://cdn.jsdelivr.net/npm/@fontsource/fira-code@5.3.0/files/fira-code-latin-',
}

async function readDownload(url: string): Promise<ArrayBuffer> {
  let cache: Cache | undefined
  try {
    cache = await caches.open(CACHE)
    const saved = await cache.match(url)
    if (saved?.ok) return await saved.arrayBuffer()
  } catch { /* Private/unsupported storage: use HTTP cache. */ }
  try {
    const saved = await fontDataCache(url)
    if (saved) return saved
  } catch { /* IndexedDB is also optional in private/restricted WebViews. */ }
  const response = await fetch(url, {signal: AbortSignal.timeout(30_000)})
  if (!response.ok) throw new Error(t("字体下载失败 ({0})", [response.status]))
  const copy = response.clone()
  const data = await response.arrayBuffer()
  let saved = false
  try { if (cache) { await cache.put(url, copy); saved = true } } catch { /* Try IndexedDB below. */ }
  if (!saved) {
    try { await fontDataCache(url, data) } catch { /* Storage quota must not prevent font use. */ }
  }
  return data
}

function download(url: string): Promise<ArrayBuffer> {
  const existing = downloads.get(url)
  if (existing) return existing
  const loading = readDownload(url).finally(() => downloads.delete(url))
  downloads.set(url, loading)
  return loading
}

async function evict(url: string) {
  try { await (await caches.open(CACHE)).delete(url) } catch { /* Cache Storage is optional. */ }
  try { await fontDataCache(url, null) } catch { /* IndexedDB is optional. */ }
}

async function loadFace(family: string, weight: number, url: string, unicodeRange?: string): Promise<FontFace[]> {
  const data = await download(url)
  const face = new FontFace(family, data, {weight: String(weight), ...(unicodeRange ? {unicodeRange} : {})})
  try {
    await face.load()
    const range = latinUnicodeRange(unicodeRange)
    const latin = range ? await new FontFace(`DingLatin-${family}`, data, {weight: String(weight), unicodeRange: range}).load() : undefined
    return latin ? [face, latin] : [face]
  } catch (error) {
    await evict(url) // A corrupt cached response must not poison every retry.
    throw error
  }
}

function register(faces: FontFace[]) {
  const fonts = document.fonts as FontFaceSet & {add(font: FontFace): FontFaceSet}
  for (const face of faces) fonts.add(face)
}

async function install(family: string, weight: number) {
  remoteFontProgress[`${family}:${weight}`] = {done: 0, total: 1}
  const latinSource = LATIN_SOURCES[family]
  if (latinSource) {
    register(await loadFace(family, weight, `${latinSource}${weight}-normal.woff2`))
    remoteFontProgress[`${family}:${weight}`].done = 1
    return
  }
  if (family === 'Sarasa Mono SC') {
    // Sarasa has no Medium; share Regular between 400 and 500.
    const file = weight === 700 ? 'Bold' : 'Regular'
    register(await loadFace(family, weight, `${SARASA}SarasaMonoSC-${file}.woff2`))
    remoteFontProgress[`${family}:${weight}`].done = 1
    return
  }
  const variant = weight === 700 ? 'bold' : weight === 500 ? 'medium' : 'main'
  const cssURL = `https://fontsapi.zeoseven.com/442/${variant}/result.css`
  const css = new TextDecoder().decode(await download(cssURL))
  const blocks = [...css.matchAll(/@font-face\s*\{([^}]+)\}/g)]
  if (!blocks.length) {
    await evict(cssURL)
    throw new Error(t("字体源未返回字体定义"))
  }
  const faces: {url: string; range?: string}[] = []
  try {
    for (const [, block] of blocks) {
      const source = block.match(/url\(["']?([^"')]+)["']?\)/)?.[1]
      if (!source) throw new Error(t("字体源缺少文件地址"))
      const range = block.match(/unicode-range\s*:\s*([^;}]+)/)?.[1]
      faces.push({url: new URL(source, cssURL).href, range})
    }
  } catch (error) {
    await evict(cssURL)
    throw error
  }
  // Fetch complete coverage, not only the preview characters: new terminal output
  // must never enter the glyph atlas before its Chinese/icon shard has loaded.
  remoteFontProgress[`${family}:${weight}`].total = faces.length
  const loaded: FontFace[][] = new Array(faces.length)
  let next = 0
  await Promise.all(Array.from({length: Math.min(8, faces.length)}, async () => {
    while (next < faces.length) {
      const index = next++
      const face = faces[index]
      loaded[index] = await loadFace(family, weight, face.url, face.range)
      remoteFontProgress[`${family}:${weight}`].done++
    }
  }))
  // Publish a weight only after all shards load; a failed selection keeps the
  // complete bundled fallback instead of exposing a partially installed font.
  register(loaded.flat())
}

export function ensureRemoteFont(family: string, weight: number): Promise<void> {
  if (!isRemoteFont(family)) return Promise.resolve()
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
