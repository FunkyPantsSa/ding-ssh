import {ref} from 'vue'

export const customFontSizes = ref<Record<string, number>>({})
export const customFontChinese = ref<Record<string, boolean>>({})
export const customFonts = ref<string[]>([])
const PREFIX = 'DingCustom-'
function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('ding-custom-fonts', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('fonts')
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}
async function register(name: string, data: ArrayBuffer) {
  const face = await new FontFace(name, data).load()
  ;(document.fonts as FontFaceSet & {add(face: FontFace): void}).add(face)
  const latin = await new FontFace(`DingLatin-${name}`, data, {unicodeRange: 'U+0000-024F,U+1E00-1EFF'}).load()
  ;(document.fonts as FontFaceSet & {add(face: FontFace): void}).add(latin)
  customFontSizes.value[name] = data.byteLength
  customFontChinese.value[name] = supportsChinese(name)
  if (!customFonts.value.includes(name)) customFonts.value.push(name)
}
let restored: Promise<void> | undefined
export function restoreCustomFonts() {
  return restored ??= (async () => {
    const db = await database()
    try {
      const records = await new Promise<{name: string; data: ArrayBuffer}[]>((resolve, reject) => {
        const request = db.transaction('fonts').objectStore('fonts').getAll()
        request.onsuccess = () => resolve(request.result)
        request.onerror = () => reject(request.error)
      })
      for (const record of records) await register(record.name, record.data)
    } finally { db.close() }
  })()
}
export async function importCustomFont(file: File) {
  if (!/\.(ttf|otf|woff2?)$/i.test(file.name)) throw new Error('不是字体文件，请选择 TTF、OTF、WOFF 或 WOFF2 字体。')
  validateFontHeader(await file.slice(0, 12).arrayBuffer())
  const data = await file.arrayBuffer()
  const digest = await crypto.subtle.digest('SHA-256', data)
  const id = Array.from(new Uint8Array(digest)).map(v => v.toString(16).padStart(2, '0')).join('')
  const name = `${PREFIX}${file.name.replace(/[^\p{L}\p{N} ._-]/gu, '')}-${id.slice(0, 12)}`
  // Validate before persisting or exposing the font in the selector.
  try { await new FontFace(name, data).load() }
  catch { throw new Error('不是有效的字体文件，文件可能已损坏或格式不受支持。') }
  const db = await database()
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction('fonts', 'readwrite')
      transaction.objectStore('fonts').put({name, data}, name)
      transaction.oncomplete = () => resolve()
      transaction.onerror = () => reject(transaction.error)
      transaction.onabort = () => reject(transaction.error)
    })
  } finally { db.close() }
  await register(name, data)
  return name
}

export async function removeCustomFont(name: string) {
  const db = await database()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction('fonts', 'readwrite')
      tx.objectStore('fonts').delete(name)
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
      tx.onabort = () => reject(tx.error)
    })
  } finally { db.close() }
  const fonts = document.fonts as FontFaceSet & {delete(face: FontFace): boolean; forEach(fn: (face: FontFace) => void): void}
  fonts.forEach(face => { if (face.family === name || face.family === `DingLatin-${name}`) fonts.delete(face) })
  delete customFontSizes.value[name]
  delete customFontChinese.value[name]
  customFonts.value = customFonts.value.filter(font => font !== name)
}


/** Probe actual rendered glyphs against fallback, rather than the filename. */
export function supportsChinese(family: string): boolean {
  const canvas = document.createElement('canvas')
  canvas.width = 128; canvas.height = 72
  const context = canvas.getContext('2d', {willReadFrequently: true})
  if (!context) return false
  const render = (font: string, character: string) => {
    context.clearRect(0, 0, 128, 72)
    context.font = `40px ${font}`
    context.fillText(character, 8, 50)
    return context.getImageData(0, 0, 128, 72).data
  }
  return [...'中文你好世界'].every(character => {
    const baseline = render('monospace', character)
    const actual = render(`"${family}", monospace`, character)
    return actual.some((value, index) => value !== baseline[index])
  })
}


export function validateFontHeader(data: ArrayBuffer) {
  if (data.byteLength < 12) throw new Error('不是字体文件：文件内容无效。')
  const signature = new DataView(data).getUint32(0)
  if (![0x00010000, 0x4f54544f, 0x74727565, 0x774f4646, 0x774f4632].includes(signature)) {
    throw new Error('不是字体文件，请选择 TTF、OTF、WOFF 或 WOFF2 字体。')
  }
}
export function formatFontSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 ** 2).toFixed(1)} MB`
}
