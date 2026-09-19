// 分屏布局引擎：把分屏树 + 容器尺寸展开为「叶子像素矩形」与「分隔条规格」。
// 纯函数、无 Vue 依赖，App.vue 与 SplitView.vue 共用。

import type {PaneNode, PaneRect} from '../types'

export interface DividerSpec {
  key: string
  direction: 'row' | 'column'
  parentId: string
  index: number
  x: number
  y: number
  w: number
  h: number
  /** 该分隔条所属父方向的总长度（像素），用于拖拽时把位移换算为比例 */
  axisTotal: number
}

export interface PaneLayout {
  /** 叶子 id → 像素矩形 */
  leafRects: Map<string, PaneRect>
  dividers: DividerSpec[]
}

export const DIVIDER_PX = 5

export function isLeafNode(node: PaneNode): boolean {
  return !node.children || node.children.length === 0
}

function layoutInto(
  node: PaneNode,
  rect: PaneRect,
  leaves: Map<string, PaneRect>,
  dividers: DividerSpec[],
) {
  if (isLeafNode(node)) {
    leaves.set(node.id, rect)
    return
  }
  const weights = node.children.map((c) => c.size ?? 1)
  const total = weights.reduce((a, b) => a + b, 0) || 1
  const isRow = node.direction === 'row'
  const axisLen = isRow ? rect.width : rect.height
  const gapCount = node.children.length - 1
  const gapPx = gapCount * DIVIDER_PX
  const usable = Math.max(0, axisLen - gapPx)
  let offset = 0
  node.children.forEach((child, i) => {
    const seg = (usable * weights[i]) / total
    let childRect: PaneRect
    if (isRow) {
      childRect = {left: rect.left + offset, top: rect.top, width: seg, height: rect.height, paneId: child.id}
    } else {
      childRect = {left: rect.left, top: rect.top + offset, width: rect.width, height: seg, paneId: child.id}
    }
    if (i > 0) {
      // 分隔条：占 gapPx 中每段 DIVIDER_PX，位于两子节点之间
      const divPos = isRow ? rect.left + offset - DIVIDER_PX : rect.top + offset - DIVIDER_PX
      dividers.push(
        isRow
          ? {key: `${node.id}-${i}`, direction: 'row', parentId: node.id, index: i, x: divPos, y: rect.top, w: DIVIDER_PX, h: rect.height, axisTotal: rect.width}
          : {key: `${node.id}-${i}`, direction: 'column', parentId: node.id, index: i, x: rect.left, y: divPos, w: rect.width, h: DIVIDER_PX, axisTotal: rect.height},
      )
    }
    layoutInto(child, childRect, leaves, dividers)
    offset += seg + (i < node.children.length - 1 ? DIVIDER_PX : 0)
  })
}

export function layoutPanes(panes: PaneNode, width: number, height: number): PaneLayout {
  const leafRects = new Map<string, PaneRect>()
  const dividers: DividerSpec[] = []
  if (!panes || width <= 0 || height <= 0) return {leafRects, dividers}
  layoutInto(panes, {left: 0, top: 0, width, height, paneId: panes.id}, leafRects, dividers)
  return {leafRects, dividers}
}

/** 找出绑定某标签的叶子节点（一个标签同一时刻至多在一个叶子）。 */
export function leafByTabId(node: PaneNode, tabId: string): PaneNode | null {
  if (isLeafNode(node)) {
    return node.paneActiveId === tabId ? node : null
  }
  for (const c of node.children) {
    const hit = leafByTabId(c, tabId)
    if (hit) return hit
  }
  return null
}
