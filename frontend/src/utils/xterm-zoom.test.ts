import {afterEach, describe, expect, it, vi} from 'vitest'
import {patchXtermZoomCoords} from './xterm-zoom'

function makeElement(offsetW: number, offsetH: number, rect: {left: number; top: number; width: number; height: number}) {
  const el = document.createElement('div')
  Object.defineProperty(el, 'offsetWidth', {value: offsetW, configurable: true})
  Object.defineProperty(el, 'offsetHeight', {value: offsetH, configurable: true})
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
  return el
}

/** 把元素挂到带 CSS zoom 的祖先下（jsdom 的 computed zoom 会如实返回祖先值）。 */
function mountInZoom(el: HTMLElement, zoom: number, rootRect?: {left: number; top: number}) {
  const shell = document.createElement('div')
  shell.style.zoom = String(zoom)
  if (rootRect) {
    shell.getBoundingClientRect = () => ({
      left: rootRect.left,
      top: rootRect.top,
      width: 1000,
      height: 800,
      right: rootRect.left + 1000,
      bottom: rootRect.top + 800,
      x: rootRect.left,
      y: rootRect.top,
      toJSON: () => ({}),
    }) as DOMRect
  }
  shell.appendChild(el)
  document.body.appendChild(shell)
  return el
}

function makeMouseEvent(clientX: number, clientY: number): MouseEvent {
  return {clientX, clientY, button: 0, buttons: 1, target: null} as unknown as MouseEvent
}

function makeTerm() {
  const getCoords = vi.fn((event: MouseEvent, _element?: HTMLElement, _cols?: number, _rows?: number, _isSelection?: boolean) => [event.clientX, event.clientY])
  const getMouseReportCoords = vi.fn((event: MouseEvent, _element?: HTMLElement) => ({x: event.clientX, y: event.clientY}))
  const getMouseEventScrollAmount = vi.fn((event: MouseEvent, _element?: HTMLElement) => event.clientY)
  const mouseService = {getCoords, getMouseReportCoords}
  const selectionService = {_getMouseEventScrollAmount: getMouseEventScrollAmount}
  const term = {_core: {_mouseService: mouseService, _selectionService: selectionService}}
  return {term, mouseService, selectionService, getCoords, getMouseReportCoords, getMouseEventScrollAmount}
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('patchXtermZoomCoords', () => {
  it('Chromium：gBCR 含 zoom，按实测比例折回布局坐标', () => {
    const {term, mouseService, selectionService, getCoords, getMouseReportCoords, getMouseEventScrollAmount} = makeTerm()
    expect(patchXtermZoomCoords(term, () => 1)).toEqual([
      'getCoords',
      'getMouseReportCoords',
      '_getMouseEventScrollAmount',
    ])
    // offset 200×100 但 gBCR 只有 160×80 → gBCR 处于显示坐标系
    const el = mountInZoom(makeElement(200, 100, {left: 100, top: 50, width: 160, height: 80}), 0.8)
    mouseService.getCoords(makeMouseEvent(180, 90), el, 80, 24, true)
    expect(getCoords).toHaveBeenCalledTimes(1)
    expect(getCoords.mock.calls[0][0].clientX).toBeCloseTo(200, 5)
    expect(getCoords.mock.calls[0][0].clientY).toBeCloseTo(100, 5)

    mouseService.getMouseReportCoords(makeMouseEvent(180, 90), el)
    expect(getMouseReportCoords.mock.calls[0][0].clientX).toBeCloseTo(200, 5)

    selectionService._getMouseEventScrollAmount(makeMouseEvent(180, 90), el)
    expect(getMouseEventScrollAmount.mock.calls[0][0].clientY).toBeCloseTo(100, 5)
  })

  it('WebKit：gBCR 是布局坐标、鼠标是显示坐标，按 zoom 折回布局坐标', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 1) // 只靠 DOM 实测
    // offset 与 gBCR 一致 → gBCR 未含 zoom（WKWebView），但鼠标仍是显示坐标
    const el = mountInZoom(makeElement(200, 100, {left: 100, top: 50, width: 200, height: 100}), 0.8)
    mouseService.getCoords(makeMouseEvent(180, 90), el, 80, 24, true)
    expect(getCoords.mock.calls[0][0].clientX).toBeCloseTo(225, 5)
    expect(getCoords.mock.calls[0][0].clientY).toBeCloseTo(112.5, 5)
  })

  it('WebKit：zoom 根不在视口原点时按实测原点换算', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 1)
    const el = mountInZoom(makeElement(200, 100, {left: 150, top: 80, width: 200, height: 100}), 0.8, {left: 50, top: 20})
    mouseService.getCoords(makeMouseEvent(180, 90), el, 80, 24, true)
    // 布局坐标 = 50 + (180 - 50) / 0.8
    expect(getCoords.mock.calls[0][0].clientX).toBeCloseTo(212.5, 5)
    expect(getCoords.mock.calls[0][0].clientY).toBeCloseTo(107.5, 5)
  })

  it('放大场景（Chromium，zoom > 1）按比例折算', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 1)
    const el = mountInZoom(makeElement(200, 100, {left: 0, top: 0, width: 250, height: 125}), 1.25)
    mouseService.getCoords(makeMouseEvent(150, 100), el, 80, 24, true)
    expect(getCoords.mock.calls[0][0].clientX).toBeCloseTo(120, 5)
    expect(getCoords.mock.calls[0][0].clientY).toBeCloseTo(80, 5)
  })

  it('WebKit 放大 125%：显示坐标同样按 zoom 折回布局坐标', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 1)
    const el = mountInZoom(makeElement(200, 100, {left: 0, top: 0, width: 200, height: 100}), 1.25)
    mouseService.getCoords(makeMouseEvent(150, 50), el, 80, 24, true)
    expect(getCoords.mock.calls[0][0].clientX).toBeCloseTo(120, 5)
    expect(getCoords.mock.calls[0][0].clientY).toBeCloseTo(40, 5)
  })

  it('亚像素舍入（0.9992）仍按 WebKit 布局坐标处理', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 1)
    const el = mountInZoom(makeElement(1338, 850, {left: 231.99, top: 89.96, width: 1336.99, height: 850}), 0.8)
    mouseService.getCoords(makeMouseEvent(268, 98), el, 191, 50, true)
    expect(getCoords.mock.calls[0][0].clientX).toBeCloseTo(335, 5)
    expect(getCoords.mock.calls[0][0].clientY).toBeCloseTo(122.5, 5)
  })

  it('元素未布局（offset 为 0）时无法判断坐标系，保持原样', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 0.8)
    const el = makeElement(0, 0, {left: 0, top: 0, width: 0, height: 0})
    const ev = makeMouseEvent(80, 40)
    mouseService.getCoords(ev, el, 80, 24, true)
    expect(getCoords.mock.calls[0][0]).toBe(ev)
  })

  it('缩放为 100% 时原样放行（不创建代理事件）', () => {
    const {term, mouseService, getCoords} = makeTerm()
    patchXtermZoomCoords(term, () => 1)
    const el = mountInZoom(makeElement(200, 100, {left: 100, top: 50, width: 200, height: 100}), 1)
    const ev = makeMouseEvent(180, 90)
    mouseService.getCoords(ev, el, 80, 24, true)
    expect(getCoords.mock.calls[0][0]).toBe(ev)
  })

  it('私有服务缺失时返回空数组且不抛错', () => {
    expect(patchXtermZoomCoords({}, () => 1)).toEqual([])
    expect(patchXtermZoomCoords(null, () => 1)).toEqual([])
  })
})
