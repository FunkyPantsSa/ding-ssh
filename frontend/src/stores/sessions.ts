import {defineStore} from 'pinia'
import type {PaneDirection, PaneNode, ServerNode, SessionTab, SplitDirection} from '../types'

let seq = 0

function paneId(): string {
  return `pane-${Date.now()}-${seq++}`
}

/** 是否叶子节点（children 为空）。 */
function isLeaf(node: PaneNode): boolean {
  return !node.children || node.children.length === 0
}

/** 收集树中所有叶子节点。 */
export function collectLeaves(node: PaneNode): PaneNode[] {
  if (isLeaf(node)) return [node]
  return node.children.flatMap((c) => collectLeaves(c))
}

/** 按 paneId 查找节点（含非叶子）。 */
export function findPaneById(node: PaneNode, paneId: string): PaneNode | null {
  if (node.id === paneId) return node
  for (const c of node.children) {
    const found = findPaneById(c, paneId)
    if (found) return found
  }
  return null
}

/** 递归查找绑定某标签的叶子节点。 */
export function findLeafByTab(node: PaneNode, tabId: string): PaneNode | null {
  if (isLeaf(node)) {
    return node.paneActiveId === tabId ? node : null
  }
  for (const c of node.children) {
    const hit = findLeafByTab(c, tabId)
    if (hit) return hit
  }
  return null
}

/** 把 oldId 节点替换为 newNode（沿树查找并替换）。 */
export function replacePaneIn(node: PaneNode, oldId: string, newNode: PaneNode): boolean {
  const idx = node.children.findIndex((c) => c.id === oldId)
  if (idx >= 0) {
    node.children[idx] = newNode
    return true
  }
  for (const c of node.children) {
    if (replacePaneIn(c, oldId, newNode)) return true
  }
  return false
}

/** 从树中删除节点（非叶子时保留其子节点上提一级）。 */
export function removePaneIn(node: PaneNode, paneId: string): boolean {
  const idx = node.children.findIndex((c) => c.id === paneId)
  if (idx >= 0) {
    const [removedNode] = node.children.splice(idx, 1)
    if (!isLeaf(removedNode)) {
      // 非叶子被删：其子节点上提
      node.children.splice(idx, 0, ...removedNode.children)
    }
    return true
  }
  for (const c of node.children) {
    if (removePaneIn(c, paneId)) return true
  }
  return false
}

export const useSessionsStore = defineStore('sessions', {
  state: () => ({
    tabs: [] as SessionTab[],
    activeId: '',
    sftpVisible: true, // 右侧 SFTP 面板显隐
    rightPanel: 'sftp' as 'sftp' | 'sysinfo', // 右侧面板模式
    // 进入分屏前右侧工具面板的显隐状态（取消分屏时自动恢复）。
    sftpVisibleBeforeSplit: true,
    // 分屏树：null = 未分屏（当前形态）。根节点 direction + children 递归嵌套。
    // 分屏树在「切到独立标签」时保留，只是不显示（见 splitVisible）。
    panes: null as PaneNode | null,
    // 分屏布局当前是否显示：false 时工作区显示 activeId 的独立标签，分屏组留在标签栏。
    splitVisible: false,
    // 最近一次获得焦点的分屏格（用于右键分屏/拖拽的锚点；未分屏时为空）。
    focusedPaneId: '',
    // 当前正在拖拽的标签 id（用于让分屏格子的拖放热区在拖拽期间接管 pointer 事件）。
    draggingTabId: '',
    // 是否正在拖拽「分屏组」（合并标签或格头标题），用于改方向而非普通标签拖拽。
    draggingSplitGroup: false,
  }),
  getters: {
    activeTab(): SessionTab | undefined {
      return this.tabs.find((t) => t.clientId === this.activeId)
    },
    /** 是否存在分屏布局（可能当前未显示）。 */
    isSplit(): boolean {
      return this.panes !== null
    },
    /** 工作区当前是否按分屏布局渲染。 */
    splitShown(): boolean {
      return this.panes !== null && this.splitVisible
    },
    /** 分屏状态下，当前焦点格内活跃的标签 id（未分屏时 = activeId）。 */
    activePaneTabId(): string {
      if (!this.panes) return this.activeId
      const leaf = findPaneById(this.panes, this.focusedPaneId)
      return leaf?.paneActiveId || this.activeId
    },
    /** 找出绑定某标签的叶子节点（一个标签同一时刻至多在一个叶子；未分屏返回 null）。 */
    leafByTabId(): (tabId: string) => PaneNode | null {
      return (tabId: string): PaneNode | null => {
        if (!this.panes) return null
        return findLeafByTab(this.panes, tabId)
      }
    },
  },
  actions: {
    openTab(node: ServerNode): SessionTab {
      const tab: SessionTab = {
        clientId: `tab-${Date.now()}-${seq++}`,
        sessionId: '',
        serverName: node.name || `${node.user}@${node.host}`,
        host: node.host,
        user: node.user,
        node,
        status: 'connecting',
        createdAt: Date.now(),
        sftpPath: '/',
        kind: 'ssh',
      }
      this.tabs.push(tab)
      this.activateTab(tab.clientId)
      return tab
    },
    openLocalTab(shellLabel = '本机'): SessionTab {
      const node: ServerNode = {
        id: '__local__',
        name: shellLabel,
        group: '',
        host: 'localhost',
        port: 0,
        user: '',
        authType: 'password',
        bgImage: '',
        blurAmount: 0,
        envVars: {},
      }
      const tab: SessionTab = {
        clientId: `tab-${Date.now()}-${seq++}`,
        sessionId: '',
        serverName: shellLabel,
        host: 'localhost',
        user: '',
        node,
        status: 'connecting',
        createdAt: Date.now(),
        sftpPath: '/',
        kind: 'local',
      }
      this.tabs.push(tab)
      this.activateTab(tab.clientId)
      return tab
    },
    bindSession(clientId: string, sessionId: string) {
      const tab = this.tabs.find((t) => t.clientId === clientId)
      if (tab) tab.sessionId = sessionId
    },
    /**
     * 激活标签：
     * - 该标签已在某个分屏格内 → 显示分屏并聚焦该格；
     * - 否则（含分屏时新开的终端）→ 作为独立标签全屏展示，分屏布局保留在分屏组标签里。
     */
    activateTab(clientId: string) {
      if (!this.tabs.some((t) => t.clientId === clientId)) return
      if (this.panes) {
        const bound = this.findLeafByTab(this.panes, clientId)
        if (bound) {
          this.splitVisible = true
          this.setFocusedPane(bound.id)
          return
        }
      }
      this.splitVisible = false
      this.activeId = clientId
    },

    /** 显示分屏布局（点击分屏组标签）：聚焦上次的焦点格。 */
    showSplit() {
      if (!this.panes) return
      this.splitVisible = true
      const leaf = this.findPane(this.focusedPaneId) ?? this.collectLeaves(this.panes)[0]
      if (leaf) this.setFocusedPane(leaf.id)
    },

    setStatus(clientId: string, status: SessionTab['status'], message?: string) {
      const tab = this.tabs.find((t) => t.clientId === clientId)
      if (tab) {
        tab.status = status
        if (message !== undefined) tab.message = message
      }
    },
    setSftpPath(clientId: string, dir: string) {
      const tab = this.tabs.find((t) => t.clientId === clientId)
      if (tab) tab.sftpPath = dir
    },
    closeTab(clientId: string) {
      const idx = this.tabs.findIndex((t) => t.clientId === clientId)
      if (idx < 0) return
      this.tabs.splice(idx, 1)
      if (this.activeId === clientId) {
        this.activeId = this.tabs[idx]?.clientId ?? this.tabs[idx - 1]?.clientId ?? ''
      }
      // 分屏：若该标签正显示在某格，回退到剩余未占用标签（无则置空），再归一化树。
      if (this.panes) {
        const leaf = this.findLeafByTab(this.panes, clientId)
        if (leaf) {
          leaf.paneActiveId = this.nextFreeTabId(leaf.id, clientId) ?? undefined
        }
        this.normalizeTree()
        this.syncActiveAfterClose()
      }
    },

    /** 关闭标签后校正：分屏显示态下活跃标签须落在某格内；隐藏态下若活跃标签属于分屏则回到分屏。 */
    syncActiveAfterClose() {
      if (!this.panes) return
      const bound = this.findLeafByTab(this.panes, this.activeId)
      if (!this.splitVisible) {
        if (bound) {
          this.splitVisible = true
          this.setFocusedPane(bound.id)
        }
        return
      }
      if (bound) {
        this.focusedPaneId = bound.id
        return
      }
      const leaf = this.findPane(this.focusedPaneId) ?? this.collectLeaves(this.panes)[0]
      if (leaf) {
        this.focusedPaneId = leaf.id
        this.activeId = leaf.paneActiveId ?? ''
      }
    },

    closeAll() {
      this.tabs = []
      this.activeId = ''
      this.panes = null
      this.splitVisible = false
      this.focusedPaneId = ''
      this.draggingTabId = ''
      this.draggingSplitGroup = false
    },
    moveTab(clientId: string, targetId: string, position: 'before' | 'after' = 'before') {
      if (!clientId || !targetId || clientId === targetId) return
      const fromIndex = this.tabs.findIndex((t) => t.clientId === clientId)
      const targetIndex = this.tabs.findIndex((t) => t.clientId === targetId)
      if (fromIndex < 0 || targetIndex < 0) return
      const [tab] = this.tabs.splice(fromIndex, 1)
      let insertIndex = targetIndex
      if (fromIndex < targetIndex) insertIndex -= 1
      if (position === 'after') insertIndex += 1
      this.tabs.splice(Math.max(0, Math.min(insertIndex, this.tabs.length)), 0, tab)
    },
    moveTabToEnd(clientId: string) {
      const fromIndex = this.tabs.findIndex((t) => t.clientId === clientId)
      if (fromIndex < 0) return
      const [tab] = this.tabs.splice(fromIndex, 1)
      this.tabs.push(tab)
    },
    toggleSftp() {
      this.sftpVisible = !this.sftpVisible
    },
    showRightPanel(panel: 'sftp' | 'sysinfo') {
      this.rightPanel = panel
      this.sftpVisible = true
    },

    // ============ 分屏 (Split Panes) ============

    /** 收集树中所有叶子节点。 */
    collectLeaves(node: PaneNode): PaneNode[] {
      return collectLeaves(node)
    },
    /** 按 paneId 查找节点（含非叶子）。 */
    findPane(paneId: string, node?: PaneNode | null): PaneNode | null {
      const root = node ?? this.panes
      if (!root) return null
      return findPaneById(root, paneId)
    },

    /** 递归查找绑定某标签的叶子节点。 */
    findLeafByTab(node: PaneNode, tabId: string): PaneNode | null {
      return findLeafByTab(node, tabId)
    },

    /**
     * 分屏：把 targetTabId 拆到一个新格，锚点格保留 anchorPaneId 当前内容。
     * - panes 为 null：首次分屏，锚点 = activeId（或第一个标签）。
     * - panes 非空：在 anchorPaneId 叶子处拆分。
     * - targetTabId 与锚点相同（或未指定）时，目标格回退到另一个可用标签。
     */
    splitPane(anchorPaneId: string, direction: SplitDirection, targetTabId?: string) {
      if (!this.tabs.length) return
      // 锚点标签：anchorPaneId 叶子内容（若有），否则 activeId / 第一个
      let anchorTabId = this.activeId
      if (anchorPaneId) {
        const anchorLeaf = this.findPane(anchorPaneId)
        anchorTabId = anchorLeaf?.paneActiveId || anchorTabId
      }
      // 目标标签：显式指定且不同于锚点 → 用它；否则找另一个可用标签
      const targetId = targetTabId && targetTabId !== anchorTabId
        ? targetTabId
        : this.tabs.find((t) => t.clientId !== anchorTabId)?.clientId || anchorTabId
      const isRow = direction === 'left' || direction === 'right'

      const makeLeaf = (tabId: string): PaneNode => ({
        id: paneId(),
        direction: isRow ? 'row' : 'column',
        children: [],
        paneActiveId: tabId,
      })

      if (!this.panes) {
        // 首次分屏：根 = 方向 + 两个叶子（锚点 + 目标）
        const anchorLeaf = makeLeaf(anchorTabId)
        const targetLeaf = makeLeaf(targetId)
        const leftOrUp = direction === 'left' || direction === 'up'
        this.panes = {
          id: paneId(),
          direction: isRow ? 'row' : 'column',
          children: leftOrUp ? [targetLeaf, anchorLeaf] : [anchorLeaf, targetLeaf],
        }
        this.splitVisible = true
        this.focusedPaneId = targetLeaf.id
        this.activeId = targetId
        return
      }

      // 已分屏：在 anchorPaneId 叶子处拆分
      const anchorLeaf = this.findPane(anchorPaneId)
      if (!anchorLeaf) return
      // 目标标签若已绑定在另一格：先把它从原格移走（原格回退到 free 标签或置空）
      const fromLeaf = this.findLeafByTab(this.panes, targetId)
      if (fromLeaf && fromLeaf.id !== anchorLeaf.id) {
        fromLeaf.paneActiveId = this.nextFreeTabId(fromLeaf.id, targetId) ?? undefined
      }
      const targetLeaf = makeLeaf(targetId)
      // 把 anchorLeaf 替换为 父节点(direction) + 两个子叶子
      const leftOrUp = direction === 'left' || direction === 'up'
      const parent: PaneNode = {
        id: paneId(),
        direction: isRow ? 'row' : 'column',
        children: leftOrUp ? [targetLeaf, anchorLeaf] : [anchorLeaf, targetLeaf],
      }
      this.replacePane(anchorLeaf.id, parent)
      this.splitVisible = true
      this.focusedPaneId = targetLeaf.id
      this.activeId = targetId
    },

    /** 把 oldId 节点替换为 newNode（沿树查找并替换）。 */
    replacePane(oldId: string, newNode: PaneNode, node?: PaneNode | null): boolean {
      const root = node ?? this.panes
      if (!root) return false
      return replacePaneIn(root, oldId, newNode)
    },

    /** 设置某格的活跃标签：若该标签已在其他格，则从原格移走（原格回退或置空）。 */
    setPaneActive(paneId: string, tabId: string) {
      const leaf = this.findPane(paneId)
      if (!leaf) return
      // 该标签当前所在的另一格（若有）
      if (this.panes) {
        const fromLeaf = this.findLeafByTab(this.panes, tabId)
        if (fromLeaf && fromLeaf.id !== paneId) {
          // 原格回退到第一个未被占用的标签；无则置空（显示空占位）
          fromLeaf.paneActiveId = this.nextFreeTabId(fromLeaf.id, tabId) ?? undefined
        }
      }
      leaf.paneActiveId = tabId
    },

    /**
     * 找一个未绑定到任何格子的标签 id（用于原格回退）。
     * @param excludePaneId 排除的格子（正在被操作/移动标签的原格）
     * @param excludeTabId  排除的标签（正在被移动的标签，不能算作 free 回退给原格）
     */
    nextFreeTabId(excludePaneId: string, excludeTabId?: string): string | null {
      if (!this.panes) return null
      const used = new Set<string>()
      for (const l of this.collectLeaves(this.panes)) {
        if (l.id !== excludePaneId && l.paneActiveId) used.add(l.paneActiveId)
      }
      return this.tabs.find((t) => t.clientId !== excludeTabId && !used.has(t.clientId))?.clientId ?? null
    },

    /** 设置焦点格。 */
    setFocusedPane(paneId: string) {
      this.focusedPaneId = paneId
      const leaf = this.findPane(paneId)
      if (leaf?.paneActiveId) this.activeId = leaf.paneActiveId
    },

    /** 关闭一个分屏格（叶子）。布局回退：父节点只剩 1 子则上提，整树只剩 1 叶则 panes=null。 */
    closePane(paneId: string) {
      if (!this.panes) return
      const target = this.findPane(paneId)
      if (!target) return
      // 叶子内标签不关闭，回到标签栏（本来就在标签栏）
      // 删除叶子
      const removed = this.removePane(this.panes, paneId)
      if (!removed) return
      this.normalizeTree()
    },

    /** 从树中删除节点（非叶子时保留其子节点上提一级）。 */
    removePane(node: PaneNode, paneId: string): boolean {
      return removePaneIn(node, paneId)
    },

    /** 树规范化：清理空叶子、上提单子节点、整树剩 1 叶时回退未分屏。 */
    normalizeTree() {
      if (!this.panes) return
      this.normalizeNode(this.panes)
      const leaves = this.collectLeaves(this.panes)
      if (leaves.length <= 1) {
        // 只剩一个格：回退未分屏，该格内容回到全局 activeId
        if (leaves[0]?.paneActiveId) this.activeId = leaves[0].paneActiveId
        this.panes = null
        this.splitVisible = false
        this.focusedPaneId = ''
        this.sftpVisible = this.sftpVisibleBeforeSplit
      }
    },

    normalizeNode(node: PaneNode): PaneNode | null {
      if (isLeaf(node)) {
        return node.paneActiveId ? node : null
      }
      // 递归清理子节点
      const kept: PaneNode[] = []
      for (const c of node.children) {
        const n = this.normalizeNode(c)
        if (n) kept.push(n)
      }
      node.children = kept
      if (node.children.length === 0) return null
      if (node.children.length === 1) {
        // 单子上提
        const only = node.children[0]
        only.size = node.size ?? only.size
        return only
      }
      return node
    },

    /** 拖拽标签到指定格：drop 到内部 = 切换该格会话；drop 到边缘 = 分屏。 */
    dropTabOnPane(tabId: string, paneId: string, edge: SplitDirection | null) {
      if (!this.panes) return
      if (edge) {
        // 分屏：锚点为该格，被拖标签放边缘方向
        const leaf = this.findPane(paneId)
        if (!leaf) return
        const currentTab = leaf.paneActiveId || this.activeId
        // 被拖标签与锚点标签相同则只切不拆
        if (currentTab === tabId) {
          this.setFocusedPane(paneId)
          return
        }
        this.splitPane(paneId, edge, tabId)
      } else {
        // 内部：切换该格会话
        this.setPaneActive(paneId, tabId)
        this.setFocusedPane(paneId)
      }
    },

    /**
     * 把 sourceTabId 合并到 targetTabId 所在格，开启分屏。
     * - 首次分屏（panes 为 null）：两个标签合成一组，目标标签保持为活跃标签。
     * - 已分屏：以 targetTabId 所在叶子为锚点，把 source 拆到 direction 方向。
     * - 分屏期间自动隐藏右侧工具面板，并在取消分屏时恢复。
     */
    mergeTabsForSplit(sourceTabId: string, targetTabId: string, direction: SplitDirection) {
      if (!this.tabs.length || sourceTabId === targetTabId) return
      if (!this.tabs.some((t) => t.clientId === sourceTabId)) return
      if (!this.tabs.some((t) => t.clientId === targetTabId)) return

      const isRow = direction === 'left' || direction === 'right'
      const makeLeaf = (tabId: string): PaneNode => ({
        id: paneId(),
        direction: isRow ? 'row' : 'column',
        children: [],
        paneActiveId: tabId,
      })

      if (!this.panes) {
        const sourceLeaf = makeLeaf(sourceTabId)
        const targetLeaf = makeLeaf(targetTabId)
        const leftOrUp = direction === 'left' || direction === 'up'
        this.panes = {
          id: paneId(),
          direction: isRow ? 'row' : 'column',
          children: leftOrUp ? [sourceLeaf, targetLeaf] : [targetLeaf, sourceLeaf],
        }
        this.splitVisible = true
        this.focusedPaneId = targetLeaf.id
        this.activeId = targetTabId
        this.sftpVisibleBeforeSplit = this.sftpVisible
        this.sftpVisible = false
        return
      }

      // 已分屏：target 优先用其所在叶子作为锚点格；
      // 若 target 未绑定（例如当前活跃的是独立标签），回退到焦点格 / 首个格子，避免「分屏」静默失效。
      const focused = this.findPane(this.focusedPaneId)
      const targetLeaf =
        this.findLeafByTab(this.panes, targetTabId)
        ?? [focused, ...this.collectLeaves(this.panes)].find((l) => l && l.paneActiveId !== sourceTabId)
        ?? null
      if (!targetLeaf) return
      const sourceLeafOld = this.findLeafByTab(this.panes, sourceTabId)
      if (sourceLeafOld && sourceLeafOld.id !== targetLeaf.id) {
        sourceLeafOld.paneActiveId = this.nextFreeTabId(sourceLeafOld.id, sourceTabId) ?? undefined
      }
      const newSourceLeaf = makeLeaf(sourceTabId)
      const leftOrUp = direction === 'left' || direction === 'up'
      const parent: PaneNode = {
        id: paneId(),
        direction: isRow ? 'row' : 'column',
        children: leftOrUp ? [newSourceLeaf, targetLeaf] : [targetLeaf, newSourceLeaf],
      }
      this.replacePane(targetLeaf.id, parent)
      this.splitVisible = true
      this.focusedPaneId = newSourceLeaf.id
      this.activeId = sourceTabId
    },

    /** 取消分屏：恢复每个标签独立展示，并恢复分屏前的右侧工具面板显隐。 */
    cancelSplit() {
      if (!this.panes) return
      this.panes = null
      this.splitVisible = false
      this.focusedPaneId = ''
      this.sftpVisible = this.sftpVisibleBeforeSplit
    },

    /** 重新设置分屏方向：把当前分屏组按现有叶子顺序重排为 row/column 单层树。 */
    reorientSplit(direction: PaneDirection) {
      if (!this.panes) return
      const tabs: string[] = []
      for (const leaf of this.collectLeaves(this.panes)) {
        if (leaf.paneActiveId) tabs.push(leaf.paneActiveId)
      }
      if (tabs.length < 2) return
      const currentTab = this.findPane(this.focusedPaneId)?.paneActiveId
      const children: PaneNode[] = tabs.map((tabId) => ({
        id: paneId(),
        direction,
        children: [],
        paneActiveId: tabId,
      }))
      this.panes = {id: paneId(), direction, children}
      this.splitVisible = true
      const focusLeaf = children.find((c) => c.paneActiveId === currentTab) ?? children[0]
      this.focusedPaneId = focusLeaf.id
      this.activeId = focusLeaf.paneActiveId ?? tabs[0]
    },

    /** 关闭整个分屏组（合并标签的关闭按钮）。 */
    closeSplitGroup() {
      if (!this.panes) return
      const ids = new Set<string>()
      for (const leaf of this.collectLeaves(this.panes)) {
        if (leaf.paneActiveId) ids.add(leaf.paneActiveId)
      }
      this.tabs = this.tabs.filter((t) => !ids.has(t.clientId))
      if (this.activeId && ids.has(this.activeId)) {
        this.activeId = this.tabs[0]?.clientId ?? ''
      }
      this.panes = null
      this.splitVisible = false
      this.focusedPaneId = ''
      this.sftpVisible = this.sftpVisibleBeforeSplit
    },

    /** 调整分隔条：更新 parent 下 index 位置的子节点 size（权重）。 */
    resizePane(parentId: string, index: number, deltaPct: number) {
      const parent = this.findPane(parentId)
      if (!parent || parent.children.length !== 2) return
      const a = parent.children[0]
      const b = parent.children[1]
      const aSize = a.size ?? 0.5
      let na = aSize + deltaPct
      na = Math.max(0.1, Math.min(0.9, na))
      a.size = na
      b.size = 1 - na
    },
  },
})
