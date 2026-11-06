import {afterEach, expect, it, vi} from 'vitest'
import 'vue'

afterEach(() => vi.unstubAllGlobals())

function fontEnvironment() {
  const add = vi.fn()
  vi.stubGlobal('document', {fonts: {add}})
  vi.stubGlobal('FontFace', class {
    load() { return Promise.resolve(this) }
  })
  return add
}

it('reuses a persistent font cache while offline', async () => {
  vi.resetModules()
  const add = fontEnvironment()
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
