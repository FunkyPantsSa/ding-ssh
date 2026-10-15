// 终端输出语义着色：把「输出内容是什么」映射成颜色，而不是替远端决定颜色。
//
// 分级（优先级从高到低）：错误红 / 警告黄 / 成功绿 / 信息青；vivid 档再加数值、路径、键名强调。
//
// 三条硬约束（改动时务必保持，否则会破坏终端本身的行为）：
// 1. 只注入 SGR（颜色 / 属性），绝不插入可见字符、绝不移动光标 —— 因此选区高亮、复制、命令补全
//    的屏幕取词、调试 API 读取 buffer 全都不受影响（SGR 是属性不是字符）。
// 2. 字节无损：会话输出是原始字节（Windows 本地 PowerShell 可能是 cp936/cp437 等非 UTF-8），
//    所以全程在 latin1 双射（byte ↔ 码元 0–255）上处理，绝不走 TextDecoder/TextEncoder 往返。
//    中文关键字按 UTF-8 字节序列匹配（见 utf8ToLatin1）。
// 3. 不确定的一律不着色（原样透传）：远端自带 ANSI（ls --color、grep --color 等）永远权威；
//    含非 SGR 控制序列（光标移动、清屏、OSC 标题、鼠标上报…）的行、TUI 全屏程序、超长行、
//    含其他控制字符的行、提示符 / 回显的命令行，全部原样输出。

import type {TerminalColorMode} from '../types'

/** 语义着色的颜色（SGR 参数），用命名色号以便跟随主题调色板（applyTheme 明暗切换自动生效）。 */
const C_ERROR = '31' // 红
const C_WARN = '33' // 黄
const C_SUCCESS = '32' // 绿
const C_INFO = '36' // 青
const C_NUMBER = '35' // 品红
const C_PATH = '96' // 亮青
const C_KEY = '94' // 亮蓝

const RESET = '\x1b[0m'

/** 单行着色上限：超长行（日志转储、base64 等）直接透传，避免正则在大字符串上扫。 */
const MAX_LINE = 2000
/** 未遇到换行符的挂起缓冲上限：防止无换行的流式输出无限堆积。 */
const MAX_PENDING = 8000

// ---------------------------------------------------------------------------
// latin1 双射：byte ↔ 码元 0–255
// ---------------------------------------------------------------------------

/** 256 个单字符字符串，索引 = 字节值。 */
const BYTE_TO_CHAR: string[] = (() => {
  const a: string[] = new Array(256)
  for (let i = 0; i < 256; i++) a[i] = String.fromCharCode(i)
  return a
})()

/** 字节 → latin1 字符串（每个字节恰好一个码元）。 */
function bytesToLatin1(bytes: Uint8Array): string {
  let s = ''
  const CHUNK = 1024
  for (let i = 0; i < bytes.length; i += CHUNK) {
    const end = Math.min(i + CHUNK, bytes.length)
    let part = ''
    for (let j = i; j < end; j++) part += BYTE_TO_CHAR[bytes[j]]
    s += part
  }
  return s
}

/** latin1 字符串 → 字节（只可能产出 0–255，故与 bytesToLatin1 互逆）。 */
function latin1ToBytes(s: string): Uint8Array {
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i) & 0xff
  return out
}

/** UTF-8 字符串 → latin1 字节串（用于把中文字面量变成可匹配的字节序列）。 */
function utf8ToLatin1(s: string): string {
  const enc = new TextEncoder().encode(s)
  let out = ''
  for (let i = 0; i < enc.length; i++) out += BYTE_TO_CHAR[enc[i]]
  return out
}

function sgr(code: string): string {
  return '\x1b[' + code + 'm'
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// ---------------------------------------------------------------------------
// 语义规则
// ---------------------------------------------------------------------------

/**
 * ASCII 关键字：要求词边界。边界类里带上 0x80–0xff，这样关键字不会被「中文的 UTF-8 字节」
 * 夹住误匹配（例如某个汉字的字节里恰好出现 ok / error）。
 * 不含 `.` 与 `-`：`error.log`、`failed-job` 这类文件名 / 任务名同样该上色，
 * 而 `TypeError`、`error_code` 因为紧邻字母 / 下划线仍然不会被误判。
 */
const ASCII_EDGE = '[A-Za-z0-9_\\u0080-\\u00ff]'

function asciiRe(words: string[]): RegExp {
  // 长词优先：permission denied 要压过 denied，timed out 要压过 out
  const alt = [...words].sort((a, b) => b.length - a.length).map(escapeRe).join('|')
  // 不带 g：只用于 test()，带 g 会因 lastIndex 残留导致「同一行第二次检测失败」
  return new RegExp('(?<!' + ASCII_EDGE + ')(?:' + alt + ')(?!' + ASCII_EDGE + ')', 'i')
}

/** 中文关键字：直接按 UTF-8 字节序列匹配，无需词边界（汉字之间没有西式分词）。 */
function cjkRe(words: string[]): RegExp | null {
  if (!words.length) return null
  const alt = [...words].sort((a, b) => b.length - a.length).map((w) => escapeRe(utf8ToLatin1(w))).join('|')
  return new RegExp('(?:' + alt + ')')
}

interface Category {
  code: string
  ascii: RegExp
  cjk: RegExp | null
}

/** 类别按优先级排列：错误 > 警告 > 成功 > 信息（命中即止）。 */
const CATEGORIES: Category[] = [
  {
    code: C_ERROR,
    ascii: asciiRe([
      'permission denied',
      'command not found',
      'segmentation fault',
      'connection refused',
      'connection reset',
      'no space left',
      'read-only file system',
      'operation not permitted',
      'no such file',
      'not found',
      'timed out',
      'out of memory',
      'core dumped',
      'syntax error',
      'errors',
      'error',
      'errored',
      'failed',
      'failure',
      'failures',
      'fatal',
      'panic',
      'panicked',
      'exception',
      'traceback',
      'aborted',
      'denied',
      'refused',
      'unreachable',
      'unauthorized',
      'forbidden',
      'invalid',
      'cannot',
      'can not',
      "can't",
      'unable',
      'missing',
      'crash',
      'crashed',
      'corrupt',
      'corrupted',
      'broken',
      'timeout',
      'overflow',
      'deadlock',
      'EPERM',
      'EACCES',
      'ENOENT',
      'ECONNREFUSED',
      'ENOTFOUND',
    ]),
    cjk: cjkRe([
      '错误',
      '出错',
      '报错',
      '失败',
      '异常',
      '无法',
      '不能',
      '拒绝',
      '无效',
      '非法',
      '未找到',
      '找不到',
      '不存在',
      '超时',
      '崩溃',
      '致命',
      '溢出',
      '死锁',
      '权限不足',
      '没有权限',
      '拒绝访问',
    ]),
  },
  {
    code: C_WARN,
    ascii: asciiRe([
      'be careful',
      'not recommended',
      'already exists',
      'deprecation',
      'deprecated',
      'warnings',
      'warning',
      'warned',
      'warn',
      'caution',
      'unstable',
      'degraded',
      'insecure',
      'outdated',
      'unsupported',
      'experimental',
      'conflicts',
      'conflict',
      'mismatch',
      'retrying',
      'retry',
      'skipped',
      'ignored',
      'overwrite',
      'fallback',
      'partial',
      'stale',
      'busy',
      'expired',
      'unverified',
      'risk',
    ]),
    cjk: cjkRe([
      '警告',
      '注意',
      '提醒',
      '建议',
      '谨慎',
      '风险',
      '危险',
      '待确认',
      '未确认',
      '未处理',
      '未验证',
      '未同步',
      '已过期',
      '过期',
      '冲突',
      '已存在',
      '覆盖',
      '重试',
      '部分',
      '不支持',
      '已忽略',
      '跳过',
      '降级',
      '不稳定',
      '实验性',
      '断开',
    ]),
  },
  {
    code: C_SUCCESS,
    ascii: asciiRe([
      'successfully',
      'successful',
      'succeeded',
      'success',
      'succeed',
      'completed',
      'complete',
      'finished',
      'installed',
      'uninstalled',
      'created',
      'deleted',
      'removed',
      'updated',
      'upgraded',
      'saved',
      'written',
      'uploaded',
      'downloaded',
      'copied',
      'moved',
      'reconnected',
      'connected',
      'passed',
      'enabled',
      'activated',
      'applied',
      'merged',
      'deployed',
      'published',
      'verified',
      'valid',
      'ready',
      'online',
      'mounted',
      'rebooted',
      'restarted',
      'done',
      'ok',
    ]),
    cjk: cjkRe([
      '成功',
      '已完成',
      '完成',
      '已安装',
      '已卸载',
      '已创建',
      '已删除',
      '已更新',
      '已升级',
      '已保存',
      '已写入',
      '已上传',
      '已下载',
      '已复制',
      '已移动',
      '已连接',
      '已启用',
      '已激活',
      '已应用',
      '已合并',
      '已部署',
      '已发布',
      '已通过',
      '校验通过',
      '已启动',
      '已停止',
      '已重启',
      '已同步',
      '最新',
      '无变更',
      '没有变更',
      '一切正常',
    ]),
  },
  {
    code: C_INFO,
    ascii: asciiRe([
      'information',
      'info',
      'hint',
      'tip',
      'usage',
      'help',
      'status',
      'version',
      'branch',
      'commit',
      'remote',
      'total',
      'available',
      'loading',
      'fetching',
      'resolving',
      'downloading',
      'uploading',
      'installing',
      'compiling',
      'building',
      'processing',
      'scanning',
      'checking',
      'searching',
      'waiting',
      'connecting',
      'starting',
      'stopping',
      'syncing',
      'refreshing',
      'estimating',
    ]),
    cjk: cjkRe([
      '信息',
      '提示',
      '状态',
      '版本',
      '用法',
      '帮助',
      '总计',
      '合计',
      '可用',
      '剩余',
      '空闲',
      '大小',
      '数量',
      '正在',
      '加载中',
      '下载中',
      '上传中',
      '安装中',
      '检查中',
      '扫描中',
      '处理中',
      '构建中',
      '编译中',
      '同步中',
      '刷新中',
      '等待中',
      '搜索中',
    ]),
  },
]

/** vivid 档的强调规则（在语义类别之外额外上色）。 */
interface EmphasisRule {
  code: string
  re: RegExp
  /** 只强调捕获组（组内容须是匹配结果的尾部，如 lookahead 收尾的键名）。 */
  group?: number
  /** 只在没有命中语义类别时强调（避免抢了错误/警告的主色）。 */
  onlyWhenPlain?: boolean
}

/** 数字（含千位分隔与小数）、百分比、路径、键名。 */
const EMPHASIS: EmphasisRule[] = [
  {
    // 123 / 1,024 / 3.14 / 45%
    code: C_NUMBER,
    re: /(?<![A-Za-z0-9_./-])\d[\d,]*(?:\.\d+)?%?(?![A-Za-z0-9_./-])/g,
  },
  {
    // /usr/local/bin、/etc/nginx/nginx.conf（行首或空白/引号/括号之后）
    code: C_PATH,
    re: /(?:^|(?<=[\s'"(=]))((?:\/[\w.+@-]+){2,}\/?)/g,
    group: 1,
  },
  {
    // key: value 里的键名
    code: C_KEY,
    re: /(?<![A-Za-z0-9_.-])([A-Za-z_][\w.-]{0,31})(?=:\s)/g,
    group: 1,
    onlyWhenPlain: true,
  },
]

// ---------------------------------------------------------------------------
// 控制序列与安全闸门
// ---------------------------------------------------------------------------

/** 完整 CSI 序列（含终止字节 0x40–0x7e）。 */
const CSI_ANCHOR = /^\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]/
/** SGR（以 m 结尾的 CSI）。 */
const SGR_ANCHOR = /^\x1b\[[\x30-\x3f]*m/
/** 除制表符外的 C0 控制字符与 DEL。 */
const CTRL_RE = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/

/** 下一行边界（\n 或 \r）。 */
function nextBreak(s: string, from: number): number {
  for (let i = from; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (c === 0x0a || c === 0x0d) return i
  }
  return -1
}

/** 扫描一行里的转义序列：是否已有 SGR、是否存在非 SGR 序列（后者一律透传）。 */
function scanEscapes(line: string): {sgr: boolean; unsafe: boolean} {
  let sgr = false
  let i = 0
  for (;;) {
    const esc = line.indexOf('\x1b', i)
    if (esc < 0) return {sgr, unsafe: false}
    const rest = line.slice(esc)
    const csi = CSI_ANCHOR.exec(rest)
    // 非 CSI（OSC 标题、字符集选择、未完成的序列…）→ 不安全
    if (!csi) return {sgr, unsafe: true}
    // CSI 但不是 SGR（光标移动、清屏、隐藏光标、鼠标上报…）→ 不安全
    if (!SGR_ANCHOR.test(csi[0])) return {sgr, unsafe: true}
    sgr = true
    i = esc + csi[0].length
  }
}

/** 提示符行尾（光标停在提示符后，或命令回显行没匹配上时兜底）：不着色。 */
const PROMPT_TAIL_RES: RegExp[] = [
  /[$#]\s*$/,
  /PS\s+[^>]{0,120}>\s*$/,
  /^[A-Za-z]:\\[^>]{0,120}>\s*$/,
]

// ---------------------------------------------------------------------------
// Colorizer
// ---------------------------------------------------------------------------

interface Span {
  start: number
  end: number
  code: string
}

/**
 * 会话级语义着色器：一个终端实例一个（alt-buffer 闸门是会话状态，不能跨会话共享）。
 *
 * 用法（TerminalView）：
 *   const colorize = new Colorizer(settings.terminalColorMode)
 *   term.write(colorize.feed(bytes))      // 输出
 *   colorize.noteCommand(cmd)             // 回车执行时记录命令，避免回显行被着色
 *   colorize.setMode(mode)                // 档位切换（返回需要补写的挂起字节）
 */
export class Colorizer {
  private mode: TerminalColorMode
  /** 未完成的半行（latin1）。 */
  private pending = ''
  /** 上一块尾巴，用于跨块检测 alt-buffer 切换序列。 */
  private tail = ''
  /** 全屏 TUI（vim/top/less…）闸门：进入 alt-buffer 后原样透传。 */
  private raw = false
  /** 最近执行的命令（latin1 + 词边界正则），用于跳过回显行。 */
  private commands: RegExp[] = []

  constructor(mode: TerminalColorMode = 'auto') {
    this.mode = mode
    if (mode === 'off') this.raw = true
  }

  /** 切换档位；返回需要补写出去的挂起字节（调用方负责 term.write）。 */
  setMode(mode: TerminalColorMode): Uint8Array {
    if (mode === this.mode) return EMPTY
    this.mode = mode
    if (mode === 'off') {
      this.raw = true
      const out = latin1ToBytes(this.pending)
      this.pending = ''
      return out
    }
    this.raw = false
    // 从 off / raw 回到着色档：丢弃在闸门里攒下的半行，避免把 TUI 残留当普通行处理
    this.pending = ''
    return EMPTY
  }

  /** 记录刚执行的命令：命中该命令的行视为回显 / 提示符行，不着色。 */
  noteCommand(cmd: string): void {
    const one = cmd.trim().split(/\r?\n/).filter(Boolean).pop()?.trim() ?? ''
    if (one.length < 2) return
    const re = new RegExp('(?<!' + ASCII_EDGE + ')(?:' + escapeRe(utf8ToLatin1(one)) + ')(?!' + ASCII_EDGE + ')')
    this.commands.push(re)
    if (this.commands.length > 12) this.commands.shift()
  }

  /** 重置会话状态（重连、清屏重建）。 */
  reset(): void {
    this.pending = ''
    this.tail = ''
    this.raw = this.mode === 'off'
    this.commands = []
  }

  /** 会话结束 / 关闭时把挂起的半行补写出去。 */
  flush(): Uint8Array {
    if (!this.pending) return EMPTY
    const line = this.pending
    this.pending = ''
    if (this.raw || this.mode === 'off') return latin1ToBytes(line)
    return latin1ToBytes(this.render(line))
  }

  /** 处理一块输出字节，返回（可能被注入颜色的）字节。 */
  feed(bytes: Uint8Array): Uint8Array {
    if (this.mode === 'off' || !bytes.length) return bytes
    const chunk = bytesToLatin1(bytes)

    // alt-buffer 切换可能跨块，用上一块的尾巴拼着扫；set/clear 幂等，重复扫无副作用
    this.scanLatch(this.tail + chunk)
    this.tail = chunk.length > 16 ? chunk.slice(-16) : chunk

    if (this.raw) {
      // 闸门内（TUI / off）原样透传；退出闸门在下一块生效
      if (!this.pending) return bytes
      const out = latin1ToBytes(this.pending + chunk)
      this.pending = ''
      return out
    }

    let s = this.pending + chunk
    this.pending = ''
    let out = ''
    let start = 0
    for (;;) {
      const brk = nextBreak(s, start)
      if (brk < 0) break
      out += this.render(s.slice(start, brk)) + s[brk]
      start = brk + 1
    }
    let rest = s.slice(start)
    // 挂起的半行：像提示符就立刻原样输出（否则用户会觉得「提示符/输入回显卡住」）；
    // 攒得太长也直接输出，避免无换行的流式输出一直不出屏。
    if (rest.length > MAX_PENDING || this.looksLikePrompt(rest) || ESC_INCOMPLETE.test(rest)) {
      out += rest
      rest = ''
    }
    this.pending = rest
    return latin1ToBytes(out)
  }

  // -- 内部 -----------------------------------------------------------------

  /** 扫描 alt-buffer 进入 / 退出序列，切换闸门。 */
  private scanLatch(s: string): void {
    const re = /\x1b\[\?(?:1049|1047|47)([hl])/g
    let m: RegExpExecArray | null
    while ((m = re.exec(s))) this.raw = m[1] === 'h'
  }

  /** 判断挂起半行是否已经是「等待输入的提示符」。 */
  private looksLikePrompt(s: string): boolean {
    return PROMPT_TAIL_RES.some((re) => re.test(s))
  }

  /** 着色一行（不含行尾换行符）。 */
  private render(line: string): string {
    if (!line || line.length > MAX_LINE) return line
    const {sgr: hasSgr, unsafe} = scanEscapes(line)
    // 远端自带颜色 / 存在光标控制等非 SGR 序列 → 原样透传
    if (hasSgr || unsafe) return line
    if (CTRL_RE.test(line)) return line
    if (this.isEchoLine(line)) return line

    const category = this.pickCategory(line)
    const base = category?.code ?? null
    const spans = this.mode === 'vivid' ? this.emphasis(line, base) : []
    if (!base && !spans.length) return line
    return renderSpans(line, base, spans)
  }

  /** 是否是命令回显 / 提示符行。 */
  private isEchoLine(line: string): boolean {
    for (const re of this.commands) if (re.test(line)) return true
    return this.looksLikePrompt(line)
  }

  /** 取优先级最高的命中类别。 */
  private pickCategory(line: string): Category | null {
    for (const cat of CATEGORIES) {
      if (cat.ascii.test(line) || (cat.cjk ? cat.cjk.test(line) : false)) return cat
    }
    return null
  }

  /** vivid：收集数值 / 路径 / 键名强调区间。 */
  private emphasis(line: string, base: string | null): Span[] {
    const spans: Span[] = []
    for (const rule of EMPHASIS) {
      if (rule.onlyWhenPlain && base) continue
      rule.re.lastIndex = 0
      let m: RegExpExecArray | null
      while ((m = rule.re.exec(line))) {
        const text = rule.group ? m[rule.group] : m[0]
        if (!text) {
          if (rule.re.lastIndex <= m.index) rule.re.lastIndex = m.index + 1
          continue
        }
        // 组内容一定是匹配结果的尾部（lookahead / 前缀断言收尾），据此反推区间
        const end = m.index + m[0].length
        const start = end - text.length
        if (!spans.some((sp) => start < sp.end && end > sp.start)) spans.push({start, end, code: rule.code})
        if (rule.re.lastIndex <= m.index) rule.re.lastIndex = m.index + 1
      }
    }
    return spans
  }
}

const EMPTY = new Uint8Array(0)
/** 半行以 ESC 开头又还没成形：先别下结论，等下一块。 */
const ESC_INCOMPLETE = /\x1b(\[[\x30-\x3f]*[\x20-\x2f]*)?$/

/** 按 base 色铺底、span 覆盖强调后拼接 SGR。 */
function renderSpans(line: string, base: string | null, spans: Span[]): string {
  const sorted = [...spans].sort((a, b) => a.start - b.start || a.end - b.end)
  let out = base ? sgr(base) : ''
  let cursor = 0
  for (const sp of sorted) {
    if (sp.start < cursor) continue
    out += line.slice(cursor, sp.start) + sgr(sp.code) + line.slice(sp.start, sp.end) + sgr(base ?? '0')
    cursor = sp.end
  }
  out += line.slice(cursor)
  if (base) out += RESET
  return out
}
