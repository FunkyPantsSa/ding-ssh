import {describe, expect, it, vi} from 'vitest'
import {invalidateWebglGlyphModel} from './xterm-webgl-zoom'

function fakeAddon(clearModel: (clearGlyphs?: boolean) => void) {
  return {
    _renderer: {
      _canvas: {},
      _setCanvasDevicePixelDimensions: vi.fn(),
      handleResize: vi.fn(),
      _clearModel: clearModel,
    },
  }
}

describe('invalidateWebglGlyphModel', () => {
  it('clears the renderer vertex model when the private API is available', () => {
    const clearModel = vi.fn()
    expect(invalidateWebglGlyphModel(fakeAddon(clearModel))).toBe(true)
    expect(clearModel).toHaveBeenCalledWith(true)
  })

  it('reports failure when the addon has no renderer model API', () => {
    expect(invalidateWebglGlyphModel(null)).toBe(false)
    expect(invalidateWebglGlyphModel({})).toBe(false)
    expect(invalidateWebglGlyphModel({_renderer: {}})).toBe(false)
  })

  it('never throws when the private API changes behavior', () => {
    const addon = fakeAddon(() => {
      throw new Error('private API changed')
    })
    expect(invalidateWebglGlyphModel(addon)).toBe(false)
  })
})
