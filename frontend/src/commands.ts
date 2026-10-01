// 命令面板的**唯一定义**（M9 抽取）。
//
// 背景：命令面板的定义原先在 App.vue 的组件内部（cmdItems）与 debug/bridge.ts 里各维护一份，
// 改一处忘另一处就会出现「面板里有这条命令、AI 调 app.command 却报未知命令」这类不一致。
// 现在两侧都从这里取：App.vue 用 buildCommands 渲染面板，bridge.ts 用它实现 app.commands / app.command。
//
// 约束（M9 之前的行为必须保持）：
//   - id 与顺序不变：workspace / servers / tunnel / settings / new / local /
//     hide-tool（侧栏已展开时）或 show-sftp + show-sys（侧栏收起时）/ 每台服务器 connect-<serverId>；
//   - 标题（title）与影响范围说明（note）不变；
//   - 执行动作与返回值不变（面板忽略返回值，app.command 把它回给调用方）。
//
// 影响范围与能力判定（cap / confirm）与 Go 侧 internal/debugsrv/appctl_register.go 的
// appctlCommandRules 对齐：现有命令全部只切换视图 / 打开面板 / 建立会话（不写配置、不断开已有会话），
// 因此不需要能力位与两段式确认；未登记的命令在 Go 侧按最保守策略（config.write + 确认）处理。

import {useSessionsStore} from './stores/sessions'
import {useServersStore} from './stores/servers'
import {useUIStore} from './stores/ui'

/** 一条命令的影响范围判定。 */
export interface CommandEffect {
  /** 需要的能力位（'' = 只读 / 切换视图，不需要能力位） */
  cap: '' | 'config.write' | 'lifecycle'
  /** 是否需要两段式确认 token（confirm.prepare → confirm.commit） */
  confirm: boolean
  /** 给 AI / 用户看的中文影响范围说明 */
  note: string
}

/** 命令面板 / app.commands 共用的一条命令。 */
export interface AppCommand extends CommandEffect {
  id: string
  title: string
  section: string
  hotkey?: string
  /** 执行命令并返回结果（面板忽略返回值；app.command 会把它回给调用方） */
  run: () => unknown
}

/** buildCommands 需要的 store（由调用方注入，便于复用与测试）。 */
export interface CommandContext {
  sessions: ReturnType<typeof useSessionsStore>
  servers: ReturnType<typeof useServersStore>
  ui: ReturnType<typeof useUIStore>
}

const VIEW_EFFECT: CommandEffect = {cap: '', confirm: false, note: '切换视图（只读 / 可逆）'}

/** 已登记命令的影响范围（与 Go 侧 appctlCommandRules 的键集一致）。 */
const commandEffects: Record<string, CommandEffect> = {
  workspace: VIEW_EFFECT,
  servers: VIEW_EFFECT,
  tunnel: VIEW_EFFECT,
  settings: VIEW_EFFECT,
  new: {cap: '', confirm: false, note: '打开新建服务器对话框（不写配置）'},
  local: {cap: '', confirm: false, note: '会新建一个本机终端标签'},
  'hide-tool': {cap: '', confirm: false, note: '收起右侧工具侧栏'},
  'show-sftp': {cap: '', confirm: false, note: '展开右侧 SFTP 面板'},
  'show-sys': {cap: '', confirm: false, note: '展开右侧系统看板'},
}

/** 前缀命令的影响范围（按服务器动态生成的 connect-<serverId>）。 */
const commandPrefixEffects: {prefix: string; effect: CommandEffect}[] = [
  {prefix: 'connect-', effect: {cap: '', confirm: false, note: '会新建一个 SSH 会话标签'}},
]

/** commandEffect 返回某条命令的影响范围；未登记的命令按最保守策略（与 Go 侧一致）。 */
export function commandEffect(id: string): CommandEffect {
  const key = id.trim()
  const hit = commandEffects[key]
  if (hit) return hit
  for (const p of commandPrefixEffects) {
    if (key.startsWith(p.prefix)) return p.effect
  }
  return {
    cap: 'config.write',
    confirm: true,
    note: '未登记的命令：按最保守策略处理（要求 config.write 能力位 + 两段式确认）',
  }
}

/** buildCommands 生成当前命令清单（含按已保存服务器动态生成的 connect-<id>）。 */
export function buildCommands(ctx: CommandContext): AppCommand[] {
  const {sessions, servers, ui} = ctx
  const items: AppCommand[] = [
    {
      id: 'workspace', title: '打开工作区', section: '导航', ...VIEW_EFFECT,
      run: () => {
        ui.showWorkspace()
        return {ok: true, view: ui.view}
      },
    },
    {
      id: 'servers', title: '打开服务器管理', section: '导航', ...VIEW_EFFECT,
      run: () => {
        ui.showServers()
        return {ok: true, view: ui.view}
      },
    },
    {
      id: 'tunnel', title: '打开隧道页', section: '导航', ...VIEW_EFFECT,
      run: () => {
        ui.showTunnel()
        return {ok: true, view: ui.view}
      },
    },
    {
      id: 'settings', title: '打开设置', section: '导航', ...VIEW_EFFECT,
      run: () => {
        ui.showSettings()
        return {ok: true, view: ui.view}
      },
    },
    {
      id: 'new', title: '新建服务器', section: '操作', ...commandEffect('new'),
      run: () => {
        ui.requestNewServer()
        return {ok: true, view: ui.view}
      },
    },
    {
      id: 'local', title: '打开本地终端', section: '工作区', ...commandEffect('local'),
      run: () => {
        ui.showWorkspace()
        sessions.openLocalTab()
        return {ok: true, kind: 'local'}
      },
    },
  ]
  if (sessions.sftpVisible) {
    items.push({
      id: 'hide-tool', title: '收起侧栏工具', section: '工作区', ...commandEffect('hide-tool'),
      run: () => {
        sessions.sftpVisible = false
        return {ok: true}
      },
    })
  } else {
    items.push({
      id: 'show-sftp', title: '打开 SFTP', section: '工作区', ...commandEffect('show-sftp'),
      run: () => {
        sessions.showRightPanel('sftp')
        return {ok: true}
      },
    })
    items.push({
      id: 'show-sys', title: '打开系统看板', section: '工作区', ...commandEffect('show-sys'),
      run: () => {
        sessions.showRightPanel('sysinfo')
        return {ok: true}
      },
    })
  }
  // 每台已保存的服务器一条 connect-<serverId>。
  // 这里不再像 bridge.ts 原先那样截断到 500 台：Go 侧（appctlMaxListItems=500）会对
  // app.commands 的结果统一截断并标注 truncated，面板则应当列出用户实际拥有的全部服务器。
  for (const s of servers.servers) {
    items.push({
      id: 'connect-' + s.id,
      title: '连接 ' + (s.name || `${s.user}@${s.host}`),
      section: '工作区',
      ...commandEffect('connect-' + s.id),
      run: () => {
        ui.showWorkspace()
        sessions.openTab(s)
        return {ok: true, kind: 'ssh', serverId: s.id}
      },
    })
  }
  return items
}

/**
 * 执行一条命令：与命令面板走同一个 run 闭包（不会出现「面板能点、AI 说未知命令」）。
 *
 * 现有内置命令都不接受参数，因此传了非空 args 时显式报错（而不是静默忽略）。
 */
export function runCommand(ctx: CommandContext, id: string, extra?: unknown): unknown {
  if (extra && typeof extra === 'object' && Object.keys(extra as object).length > 0) {
    throw new Error(`命令 ${id} 不接受参数`)
  }
  const key = id.trim()
  const cmd = buildCommands(ctx).find((c) => c.id === key)
  if (!cmd) {
    throw new Error(`未知命令: ${id}（可用 app.commands 查看当前命令）`)
  }
  return cmd.run()
}
