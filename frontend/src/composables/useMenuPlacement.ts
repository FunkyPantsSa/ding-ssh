// 右键菜单的视口内定位：按菜单**真实尺寸**贴边 / 翻转 / 限高，避免靠窗口底部或右侧时被裁掉。
//
// 背景：这些菜单都是 `Teleport to="body"` + `position: fixed`，坐标来自鼠标 clientX/Y。
// 旧实现用写死的尺寸估高（160 / 250 / 320px），菜单实际更高时就会被窗口边缘截断
// （最典型的是标签右键菜单：实际约 300px，贴底保护只按 160px 算）。
//
// 这里统一在菜单渲染后测量自身尺寸再定位：
//   1. 放得下 → 保持点击位置；
//   2. 下方放不下 → 上移到贴底（子菜单有锚点时翻到锚点上方）；
//   3. 右侧放不下 → 贴右（子菜单有锚点时翻到锚点左侧；flipX:false 可关闭）；
//   4. 菜单比视口还高 → 贴顶 + 限高滚动，至少保证所有项都能访问到。
import {nextTick, ref} from 'vue'

export const MENU_VIEWPORT_MARGIN = 8
/** 子菜单翻转时与锚点保留的间距。 */
export const MENU_ANCHOR_GAP = 4

/** 子菜单的触发器矩形（通常是主菜单里的一行）。 */
export interface MenuAnchor {
  left: number
  top: number
  right: number
  bottom: number
}

export interface MenuPlacementOptions {
  margin?: number
  /** 子菜单锚点：右侧 / 下方放不下时翻到锚点左侧 / 上方。 */
  anchor?: MenuAnchor | null
  /** 是否允许在右侧放不下时翻到锚点左侧（下拉选择器通常只需贴边，保持左对齐）。 */
  flipX?: boolean
  gap?: number
}

export interface MenuPlacement {
  left: number
  top: number
  /** 菜单高于视口时返回可用高度（配合 overflow-y:auto），否则为 null。 */
  maxHeight: number | null
}

/** 纯计算：给定菜单尺寸与期望坐标，返回不会超出视口的最终位置。 */
export function fitMenuPlacement(
  size: {width: number; height: number},
  x: number,
  y: number,
  options: MenuPlacementOptions = {},
): MenuPlacement {
  const margin = options.margin ?? MENU_VIEWPORT_MARGIN
  const gap = options.gap ?? MENU_ANCHOR_GAP
  const vw = window.innerWidth
  const vh = window.innerHeight
  const width = Math.max(0, size.width)
  const height = Math.max(0, size.height)

  let left = x
  let top = y
  let maxHeight: number | null = null

  // 水平：优先放在指针/锚点右侧；子菜单右侧放不下时翻到锚点左侧；再不行贴边。
  if (options.flipX !== false && options.anchor && left + width > vw - margin) {
    const flipped = options.anchor.left - width - gap
    if (flipped >= margin) left = flipped
  }
  left = Math.min(Math.max(margin, left), Math.max(margin, vw - width - margin))

  // 垂直：下方放不下时上移；子菜单优先翻到锚点上方。
  if (top + height > vh - margin) {
    if (options.anchor && options.anchor.top - height - gap >= margin) {
      top = options.anchor.top - height - gap
    } else {
      top = Math.max(margin, vh - height - margin)
    }
  }
  if (top + height > vh - margin) {
    top = margin
    maxHeight = Math.max(80, vh - margin * 2)
  }
  return {left, top, maxHeight}
}

/**
 * 菜单定位组合式：把 `menuEl` 绑到菜单根节点、`menuStyle` 绑到样式，
 * 打开时调用 `place(x, y, options)` 即可。测量发生在 nextTick（浏览器绘制前），
 * 不会出现「先渲染到错误位置再跳」的可见抖动。
 */
export function useMenuPlacement() {
  const menuEl = ref<HTMLElement | null>(null)
  const menuStyle = ref<Record<string, string>>({})
  let seq = 0

  async function place(x: number, y: number, options: MenuPlacementOptions = {}) {
    const id = ++seq
    // 先按点击位置渲染一帧，才能量到菜单的自然尺寸；下一次渲染会校正。
    menuStyle.value = {left: `${x}px`, top: `${y}px`}
    await nextTick()
    if (id !== seq) return
    const el = menuEl.value
    if (!el) return
    const rect = el.getBoundingClientRect()
    const placement = fitMenuPlacement(rect, x, y, options)
    if (id !== seq) return
    const style: Record<string, string> = {
      left: `${placement.left}px`,
      top: `${placement.top}px`,
    }
    if (placement.maxHeight !== null) {
      style['max-height'] = `${placement.maxHeight}px`
      style['overflow-y'] = 'auto'
    }
    menuStyle.value = style
  }

  return {menuEl, menuStyle, place}
}
