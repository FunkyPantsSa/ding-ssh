import {afterEach, describe, expect, it} from 'vitest'
import {getDisplayRect} from './dom'

function mountZoomedChild(zoom: number): {shell: HTMLElement; child: HTMLElement} {
  const shell = document.createElement('div')
  shell.style.zoom = String(zoom)
  const child = document.createElement('div')
  shell.appendChild(child)
  document.body.appendChild(shell)
  return {shell, child}
}

function mockRect(el: HTMLElement, rect: {left: number; top: number; width: number; height: number}) {
  el.getBoundingClientRect = () => ({
    left: rect.left,
    top: rect.top,
    width: rect.width,
    height: rect.height,
    right: rect.left + rect.width,
    bottom: rect.top + rect.height,
    x: rect.left,
    y: rect.top,
    toJSON: () => rect,
  }) as DOMRect
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('getDisplayRect', () => {
  it('无缩放时原样返回视口坐标', () => {
    const {child} = mountZoomedChild(1)
    Object.defineProperty(child, 'offsetWidth', {value: 200, configurable: true})
    Object.defineProperty(child, 'offsetHeight', {value: 100, configurable: true})
    mockRect(child, {left: 20, top: 30, width: 200, height: 100})
    expect(getDisplayRect(child)).toMatchObject({left: 20, top: 30, width: 200, height: 100})
  })

  it('WKWebView：gBCR 是布局坐标，乘 zoom 得到显示坐标', () => {
    const {child} = mountZoomedChild(0.8)
    Object.defineProperty(child, 'offsetWidth', {value: 200, configurable: true})
    Object.defineProperty(child, 'offsetHeight', {value: 100, configurable: true})
    // gBCR 与 offset 一致（不含 zoom），实际显示应缩小到 0.8
    mockRect(child, {left: 100, top: 50, width: 200, height: 100})
    const rect = getDisplayRect(child)
    expect(rect.left).toBeCloseTo(80, 5)
    expect(rect.top).toBeCloseTo(40, 5)
    expect(rect.width).toBeCloseTo(160, 5)
    expect(rect.height).toBeCloseTo(80, 5)
  })

  it('WKWebView 小元素：offset 与 gBCR 存在亚像素差异时仍按布局坐标换算', () => {
    const {child} = mountZoomedChild(0.8)
    // 实际状态栏按钮：offset 31×23，gBCR 30.74×21.99（比例 0.974，接近 1 而不是 0.8）
    Object.defineProperty(child, 'offsetWidth', {value: 31, configurable: true})
    Object.defineProperty(child, 'offsetHeight', {value: 23, configurable: true})
    mockRect(child, {left: 479.98, top: 974, width: 30.74, height: 21.99})
    const rect = getDisplayRect(child)
    expect(rect.left).toBeCloseTo(479.98 * 0.8, 4)
    expect(rect.top).toBeCloseTo(974 * 0.8, 4)
    expect(rect.width).toBeCloseTo(30.74 * 0.8, 4)
  })

  it('Chromium：gBCR 已含 zoom，直接使用', () => {
    const {child} = mountZoomedChild(0.8)
    Object.defineProperty(child, 'currentCSSZoom', {value: 0.8, configurable: true})
    Object.defineProperty(child, 'offsetWidth', {value: 200, configurable: true})
    Object.defineProperty(child, 'offsetHeight', {value: 100, configurable: true})
    // gBCR 已是显示坐标（布局 200×100 × 0.8）
    mockRect(child, {left: 80, top: 40, width: 160, height: 80})
    expect(getDisplayRect(child)).toMatchObject({left: 80, top: 40, width: 160, height: 80})
  })

  it('空元素返回零矩形', () => {
    expect(getDisplayRect(null)).toEqual({left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0})
  })
})
