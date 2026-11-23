import {afterEach, beforeEach, describe, expect, it} from 'vitest'
import {fitMenuPlacement} from './useMenuPlacement'

const originalWidth = window.innerWidth
const originalHeight = window.innerHeight

beforeEach(() => {
  Object.defineProperty(window, 'innerWidth', {configurable: true, value: 1280})
  Object.defineProperty(window, 'innerHeight', {configurable: true, value: 800})
})

afterEach(() => {
  Object.defineProperty(window, 'innerWidth', {configurable: true, value: originalWidth})
  Object.defineProperty(window, 'innerHeight', {configurable: true, value: originalHeight})
})

describe('fitMenuPlacement', () => {
  it('空间足够时保持点击位置', () => {
    expect(fitMenuPlacement({width: 200, height: 300}, 100, 100)).toEqual({left: 100, top: 100, maxHeight: null})
  })

  it('靠窗口底部时整体上移，菜单完整可见', () => {
    const placed = fitMenuPlacement({width: 200, height: 300}, 100, 700)
    expect(placed.top).toBe(492) // 800 - 300 - 8
    expect(placed.top + 300).toBeLessThanOrEqual(800 - 8)
    expect(placed.maxHeight).toBeNull()
  })

  it('靠窗口右侧时贴右，不超出视口', () => {
    const placed = fitMenuPlacement({width: 200, height: 300}, 1200, 100)
    expect(placed.left).toBe(1072) // 1280 - 200 - 8
    expect(placed.left + 200).toBeLessThanOrEqual(1280 - 8)
  })

  it('菜单比视口还高时贴顶并限高滚动', () => {
    const placed = fitMenuPlacement({width: 200, height: 900}, 100, 100)
    expect(placed.top).toBe(8)
    expect(placed.maxHeight).toBe(784) // 800 - 8*2
  })

  it('子菜单右侧放不下时翻到锚点左侧', () => {
    const anchor = {left: 1050, top: 200, right: 1250, bottom: 228}
    const placed = fitMenuPlacement({width: 180, height: 150}, 1254, 200, {anchor})
    expect(placed.left).toBe(866) // 1050 - 180 - 4
    expect(placed.top).toBe(200)
  })

  it('flipX:false 时右侧放不下只贴边，不翻到锚点左侧', () => {
    const anchor = {left: 1050, top: 200, right: 1250, bottom: 228}
    const placed = fitMenuPlacement({width: 180, height: 150}, 1254, 200, {anchor, flipX: false})
    expect(placed.left).toBe(1280 - 180 - 8)
    expect(placed.top).toBe(200)
  })

  it('子菜单下方放不下时翻到锚点上方', () => {
    const anchor = {left: 600, top: 760, right: 800, bottom: 798}
    const placed = fitMenuPlacement({width: 180, height: 100}, 804, 760, {anchor})
    expect(placed.top).toBe(656) // 760 - 100 - 4
  })

  it('锚点上方也放不下时退回贴底（仍完整可见）', () => {
    const anchor = {left: 600, top: 100, right: 800, bottom: 128}
    const placed = fitMenuPlacement({width: 180, height: 700}, 804, 700, {anchor})
    expect(placed.top).toBe(92) // 800 - 700 - 8
    expect(placed.top + 700).toBeLessThanOrEqual(800 - 8)
  })
})
