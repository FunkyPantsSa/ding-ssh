import {afterEach, expect, it, vi} from 'vitest'
import 'vue'

afterEach(() => vi.unstubAllGlobals())

function fontEnvironment() {
  const add = vi.fn()
  const load = vi.fn((face: unknown) => Promise.resolve(face))
  vi.stubGlobal('document', {fonts: {add}})
  vi.stubGlobal('FontFace', class {
    constructor(public family: string, public data: ArrayBuffer, public descriptors: FontFaceDescriptors) {}
    load() { return load(this) }
  })
  return {add, load}
}

it('reuses a persistent font cache while offline', async () => {
  vi.resetModules()
  const {add} = fontEnvironment()
  const fetch = vi.fn().mockRejectedValue(new Error('offline'))
  vi.stubGlobal('fetch', fetch)
  vi.stubGlobal('caches', {open: vi.fn().mockResolvedValue({match: vi.fn().mockResolvedValue(new Response(new Uint8Array([1])))})})
  const {ensureRemoteFont} = await import('./remote-fonts')
  await ensureRemoteFont('Sarasa Mono SC', 400)
  expect(fetch).not.toHaveBeenCalled()
  expect(add).toHaveBeenCalledTimes(2) // Full coverage plus Latin-only alias.
})

it('deduplicates downloads and allows retry after a network failure', async () => {
  vi.resetModules()
  fontEnvironment()
  vi.stubGlobal('caches', {open: vi.fn().mockRejectedValue(new Error('unavailable'))})
  const fetch = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(new Response(new Uint8Array([1])))
  vi.stubGlobal('fetch', fetch)
  const {ensureRemoteFont, remoteFontStatus} = await import('./remote-fonts')
  const first = ensureRemoteFont('Sarasa Mono SC', 700)
  expect(ensureRemoteFont('Sarasa Mono SC', 700)).toBe(first)
  await expect(first).rejects.toThrow('offline')
  expect(remoteFontStatus['Sarasa Mono SC:700']).toBe('error')
  await ensureRemoteFont('Sarasa Mono SC', 700)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(remoteFontStatus['Sarasa Mono SC:700']).toBe('ready')
})

it.each([
  ['JetBrains Mono', 'jetbrains-mono'],
  ['Fira Code', 'fira-code'],
])('downloads %s only when selected and reuses the ready face', async (family, slug) => {
  vi.resetModules()
  fontEnvironment()
  vi.stubGlobal('caches', {open: vi.fn().mockRejectedValue(new Error('unavailable'))})
  const fetch = vi.fn().mockImplementation(async () => new Response(new Uint8Array([1])))
  vi.stubGlobal('fetch', fetch)
  const {ensureRemoteFont, remoteFontStatus} = await import('./remote-fonts')
  expect(fetch).not.toHaveBeenCalled()
  await ensureRemoteFont(family, 400)
  await ensureRemoteFont(family, 400)
  expect(fetch).toHaveBeenCalledOnce()
  expect(fetch.mock.calls[0][0]).toBe(`https://cdn.jsdelivr.net/npm/@fontsource/${slug}@5.3.0/files/${slug}-latin-400-normal.woff2`)
  expect(remoteFontStatus[`${family}:400`]).toBe('ready')
})

it('never contacts a font server for the default offline set or system fonts', async () => {
  vi.resetModules()
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  const {ensureRemoteFont, remoteFontStatus} = await import('./remote-fonts')
  for (const family of ['Geist Sans', 'Geist Mono', 'Noto Sans SC', 'system']) {
    await ensureRemoteFont(family, 400)
    await ensureRemoteFont(family, 700)
  }
  expect(fetch).not.toHaveBeenCalled()
  expect(remoteFontStatus).toEqual({})
})

it.each(['match', 'put'] as const)('can use a downloaded font when cache.%s fails', async method => {
  vi.resetModules()
  fontEnvironment()
  const cache = {match: vi.fn().mockResolvedValue(undefined), put: vi.fn().mockResolvedValue(undefined)}
  cache[method].mockRejectedValue(new Error('storage unavailable'))
  vi.stubGlobal('caches', {open: vi.fn().mockResolvedValue(cache)})
  const fetch = vi.fn().mockResolvedValue(new Response(new Uint8Array([1])))
  vi.stubGlobal('fetch', fetch)
  const {ensureRemoteFont, remoteFontStatus} = await import('./remote-fonts')
  await ensureRemoteFont('Fira Code', 700)
  expect(fetch).toHaveBeenCalledOnce()
  expect(remoteFontStatus['Fira Code:700']).toBe('ready')
})

it('shares the Sarasa Regular download between concurrent 400 and 500 requests', async () => {
  vi.resetModules()
  const {add} = fontEnvironment()
  vi.stubGlobal('caches', {open: vi.fn().mockRejectedValue(new Error('unavailable'))})
  const fetch = vi.fn().mockResolvedValue(new Response(new Uint8Array([1])))
  vi.stubGlobal('fetch', fetch)
  const {ensureRemoteFont} = await import('./remote-fonts')
  await Promise.all([ensureRemoteFont('Sarasa Mono SC', 400), ensureRemoteFont('Sarasa Mono SC', 500)])
  expect(fetch).toHaveBeenCalledOnce()
  expect(add).toHaveBeenCalledTimes(4)
})

it('publishes Maple only after every shard is loaded and limits Latin aliases to Latin shards', async () => {
  vi.resetModules()
  const {add, load} = fontEnvironment()
  let release!: () => void
  const waiting = new Promise<void>(resolve => { release = resolve })
  load.mockImplementation(async (face: any) => {
    if (new Uint8Array(face.data)[0] === 2) await waiting
    return face
  })
  vi.stubGlobal('caches', {open: vi.fn().mockRejectedValue(new Error('unavailable'))})
  vi.stubGlobal('fetch', vi.fn(async (url: string) => new Response(url.endsWith('.css')
    ? '@font-face {src:url(./chinese.woff2);unicode-range:U+4E00-9FFF;} @font-face {src:url(./latin.woff2);unicode-range:U+0-FF;}'
    : new Uint8Array([url.includes('latin') ? 2 : 1]))))
  const {ensureRemoteFont, remoteFontStatus, remoteFontProgress} = await import('./remote-fonts')
  const loading = ensureRemoteFont('Maple Mono NF CN', 400)
  await vi.waitFor(() => expect(remoteFontProgress['Maple Mono NF CN:400']?.done).toBe(1))
  expect(add).not.toHaveBeenCalled()
  expect(remoteFontStatus['Maple Mono NF CN:400']).toBe('loading')
  release()
  await loading
  expect(add).toHaveBeenCalledTimes(3) // Two full faces; only one shard covers Latin.
  expect(remoteFontStatus['Maple Mono NF CN:400']).toBe('ready')
})

it('evicts a corrupt cached font so retry can fetch a valid response', async () => {
  vi.resetModules()
  const {load} = fontEnvironment()
  load.mockImplementation(async (face: any) => {
    if (new Uint8Array(face.data)[0] === 0) throw new Error('invalid font')
    return face
  })
  let corrupt = true
  const cache = {
    match: vi.fn(async () => corrupt ? new Response(new Uint8Array([0])) : undefined),
    delete: vi.fn(async () => { corrupt = false; return true }),
    put: vi.fn().mockResolvedValue(undefined),
  }
  vi.stubGlobal('caches', {open: vi.fn().mockResolvedValue(cache)})
  const fetch = vi.fn().mockResolvedValue(new Response(new Uint8Array([1])))
  vi.stubGlobal('fetch', fetch)
  const {ensureRemoteFont} = await import('./remote-fonts')
  await expect(ensureRemoteFont('Fira Code', 400)).rejects.toThrow('invalid font')
  expect(cache.delete).toHaveBeenCalledOnce()
  await ensureRemoteFont('Fira Code', 400)
  expect(fetch).toHaveBeenCalledOnce()
})

it('persists downloads through IndexedDB when a native WebView has no Cache Storage', async () => {
  vi.resetModules()
  fontEnvironment()
  vi.stubGlobal('caches', undefined)
  const records = new Map<string, ArrayBuffer>()
  const close = vi.fn()
  vi.stubGlobal('indexedDB', {
    open() {
      const opening: any = {result: {
        close,
        transaction() {
          const transaction: any = {objectStore: () => ({
            get: (url: string) => operation(() => records.get(url)),
            put: (data: ArrayBuffer, url: string) => operation(() => { records.set(url, data) }),
            delete: (url: string) => operation(() => { records.delete(url) }),
          })}
          const operation = (action: () => unknown) => {
            const request: any = {}
            queueMicrotask(() => { request.result = action(); request.onsuccess?.(); transaction.oncomplete?.() })
            return request
          }
          return transaction
        },
      }}
      queueMicrotask(() => opening.onsuccess())
      return opening
    },
  })
  const fetch = vi.fn().mockResolvedValue(new Response(new Uint8Array([1])))
  vi.stubGlobal('fetch', fetch)
  await (await import('./remote-fonts')).ensureRemoteFont('Fira Code', 400)
  expect(records.size).toBe(1)
  expect(close).toHaveBeenCalledTimes(2) // Read miss, followed by a durable write.
  vi.resetModules() // Simulate reopening the app: in-memory ready/pending maps are gone.
  const offlineFetch = vi.fn().mockRejectedValue(new Error('offline'))
  vi.stubGlobal('fetch', offlineFetch)
  await (await import('./remote-fonts')).ensureRemoteFont('Fira Code', 400)
  expect(offlineFetch).not.toHaveBeenCalled()
})
