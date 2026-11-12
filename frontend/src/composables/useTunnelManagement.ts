import {computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import {onTunnelStatus, sshService} from '../services/ssh'
import {useServersStore} from '../stores/servers'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import type {TunnelInfo, TunnelStatusEvent} from '../types'

import type {TunnelMode} from '../components/tunnels/TunnelModePicker.vue'

export function useTunnelManagement() {
const servers = useServersStore()
const tunnels = ref<TunnelInfo[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const showDialog = ref(false)
const editing = ref<TunnelInfo | null>(null)
const copiedId = ref('')
const localIPv4 = ref<string[]>([])
const loadingLocalIPv4 = ref(false)
const localIPv4Error = ref(false)
const pendingActions = reactive(new Set<string>())
let copiedTimer: number | undefined
/** 载入已有隧道时暂停「按模式/服务器自动填默认值」的联动，避免覆盖原有配置。 */
let syncingForm = false
const form = reactive({
  serverId: '',
  name: '',
  mode: 'local' as TunnelMode,
  localPort: 13306,
  listenScope: 'loopback',
  listenIP: '',
  remoteHost: '127.0.0.1',
  remotePort: 3306,
})
const disposers: Array<() => void> = []

const selectedServer = computed(() => servers.servers.find((s) => s.id === form.serverId))

const listenIPOptions = computed(() => {
  const options = localIPv4.value.map(ip => ({value: ip, label: ip}))
  const saved = editing.value?.listenHost
  if (saved && form.listenIP === saved && !localIPv4.value.includes(saved)) {
    options.unshift({value: saved, label: `${saved}（原配置）`})
  }
  return options
})
watch(
  () => showDialog.value && form.mode === 'local' && form.listenScope === 'specific',
  (active) => { if (active) void loadLocalIPv4() },
)

const listenHost = computed(() => {
  if (form.mode !== 'local' || form.listenScope === 'loopback') return '127.0.0.1'
  return form.listenScope === 'all-ipv4' ? '0.0.0.0' : form.listenIP.trim()
})

function hostPort(host: string, port: number | string) {
  return `${host.includes(':') && !host.startsWith('[') ? `[${host}]` : host}:${port}`
}

const modeDetails = {
  local: {
    title: '本地转发 -L',
    summary: '从本机访问远端网络中的服务',
    description: '在本机开放一个端口，经 SSH 服务器连接只有远端网络才能访问的目标。',
    example: '例如：数据库 10.0.0.8:3306 仅内网可见。创建后访问本机 127.0.0.1:13306，即可连接该数据库。',
  },
  remote: {
    title: '远程转发 -R',
    summary: '让远端访问本机上的服务',
    description: '在 SSH 服务器上开放一个端口，收到的连接会通过隧道转发到本机服务。',
    example: '例如：本机在 3000 端口运行开发站点。远端监听 127.0.0.1:13000 后，服务器访问该地址即可打开本机站点。',
  },
  'reverse-dynamic': {
    title: '反向 SOCKS5 -R',
    summary: '让服务器应用通过本机访问网络',
    description: '通过现有 SSH 连接，在服务器的 127.0.0.1 上开 SOCKS5 入口，目标连接由本机发起。服务器须允许远程 TCP 转发。',
    example: '例如：在服务器上运行 curl --proxy socks5h://127.0.0.1:1080 https://example.com。',
  },
  dynamic: {
    title: '动态 SOCKS5 -D',
    summary: '让应用通过 SSH 服务器访问网络',
    description: '在本机创建 SOCKS5 代理，应用发出的不同目标请求都会经 SSH 服务器转发。',
    example: '例如：在浏览器中将 SOCKS5 代理设为 127.0.0.1:1080，浏览器流量就会从 SSH 服务器出口访问网络。',
  },
} as const

const currentModeDetail = computed(() => modeDetails[form.mode])

const isReverseDynamic = computed(() => form.mode === 'reverse-dynamic')
const isFixedForward = computed(() => form.mode === 'local' || form.mode === 'remote')

const hint = computed(() => {
  if (isReverseDynamic.value) {
    return `服务器 SOCKS5 代理：127.0.0.1:${form.remotePort || '?'} → 本机网络`
  }
  if (form.mode === 'dynamic') {
    return `SOCKS5 代理：127.0.0.1:${form.localPort || '?'}`
  }
  if (form.mode === 'remote') {
    return `远程转发：远端 ${form.remoteHost || '127.0.0.1'}:${form.remotePort || '?'} → 本机 127.0.0.1:${form.localPort || '?'}`
  }
  return `本地转发：${hostPort(listenHost.value || '?', form.localPort || '?')} → ${form.remoteHost || '?'}:${form.remotePort || '?'}`
})

watch(
  () => form.serverId,
  (id, old) => {
    if (syncingForm) return
    const s = servers.servers.find((x) => x.id === id)
    if (!s) return
    if (!form.name || form.name === servers.servers.find((x) => x.id === old)?.name) {
      form.name = s.name
    }
  },
)

watch(
  () => form.mode,
  (mode, previousMode) => {
    if (syncingForm) return
    if (mode === 'reverse-dynamic') form.remotePort = 1080
    if (previousMode === 'reverse-dynamic') form.remotePort = 3306
    if (mode === 'dynamic' && form.localPort === 13306) form.localPort = 1080
    if (mode === 'local' && form.localPort === 1080) form.localPort = 13306
  },
)

async function refresh() {
  loading.value = true
  try {
    tunnels.value = await sshService.listTunnels()
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function submit() {
  error.value = ''
  if (!form.serverId) {
    error.value = '请选择服务器'
    return
  }
  const node = selectedServer.value
  if (!node) return
  const localPort = isReverseDynamic.value ? 0 : Number(form.localPort)
  const remotePort = Number(form.remotePort)
  if (!isReverseDynamic.value && (!Number.isInteger(localPort) || localPort < 1 || localPort > 65535)) {
    error.value = '本地端口无效（1-65535）'
    return
  }
  if (form.mode !== 'dynamic') {
    if (!Number.isInteger(remotePort) || remotePort < 1 || remotePort > 65535) {
      error.value = '远程端口无效（1-65535）'
      return
    }
  }
  if (form.mode === 'local' && form.listenScope === 'specific' && (!listenHost.value || listenHost.value === '0.0.0.0')) {
    error.value = '请输入指定网卡的 IP 地址；通配监听请选择「所有 IPv4 网卡」'
    return
  }
  const name = form.name.trim() || node.name
  const remoteHost = isReverseDynamic.value ? '127.0.0.1' : form.remoteHost.trim() || '127.0.0.1'
  const targetPort = form.mode === 'dynamic' ? 0 : remotePort
  busy.value = true
  try {
    if (editing.value) {
      const wasRunning = editing.value.status === 'running'
      const updated = await sshService.updateTunnel(editing.value.id, node, name, form.mode, localPort, remoteHost, targetPort, listenHost.value)
      // An authentication retry can update the already-stopped replacement.
      if (wasRunning && updated.status === 'stopped') await sshService.restartTunnel(updated.id)
    } else {
      await sshService.startTunnel(node, name, form.mode, localPort, remoteHost, targetPort, listenHost.value)
    }
    await refresh()
    showDialog.value = false
    editing.value = null
  } catch (e) {
    error.value = String(e)
    await refresh()
  } finally {
    busy.value = false
  }
}

/** 用指定隧道的配置填充表单；传 null 表示按当前模式重置为新建默认值。 */
function fillForm(t: TunnelInfo | null) {
  syncingForm = true
  const host = t?.listenHost || '127.0.0.1'
  form.listenScope = host === '127.0.0.1' ? 'loopback' : host === '0.0.0.0' ? 'all-ipv4' : 'specific'
  form.listenIP = form.listenScope === 'specific' ? host : ''
  if (t) {
    form.serverId = servers.servers.some((s) => s.id === t.serverId)
      ? t.serverId
      : servers.servers[0]?.id ?? ''
    form.name = t.name
    form.mode = t.mode as TunnelMode
    form.localPort = t.localPort
    form.remoteHost = t.remoteHost || '127.0.0.1'
    form.remotePort = t.remotePort || 3306
  } else {
    if (!form.serverId && servers.servers.length) form.serverId = servers.servers[0].id
    form.name = selectedServer.value?.name ?? ''
    form.localPort = form.mode === 'dynamic' ? 1080 : 13306
    form.remoteHost = '127.0.0.1'
    form.remotePort = isReverseDynamic.value ? 1080 : 3306
  }
  void nextTick(() => {
    syncingForm = false
  })
}

function openCreate() {
  error.value = ''
  editing.value = null
  fillForm(null)
  showDialog.value = true
}

function openEdit(t: TunnelInfo) {
  error.value = ''
  editing.value = t
  fillForm(t)
  showDialog.value = true
}

function closeDialog() {
  if (busy.value) return
  showDialog.value = false
  editing.value = null
}

async function runTunnelAction(id: string, action: (id: string) => Promise<void>) {
  if (pendingActions.has(id)) return
  pendingActions.add(id)
  error.value = ''
  try {
    await action(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  } finally {
    pendingActions.delete(id)
  }
}

function onStatus(evt: TunnelStatusEvent) {
  const t = tunnels.value.find((x) => x.id === evt.id)
  if (!t) return
  t.status = evt.status
  if (evt.message) t.message = evt.message
}

const filteredTunnels = computed(() => tunnels.value.filter((t) => t.mode === form.mode))

/** 使用者真正需要填到客户端里的地址。 */
function tunnelAddress(t: TunnelInfo): string {
  if (t.mode === 'reverse-dynamic') return `socks5h://127.0.0.1:${t.remotePort}`
  if (t.mode === 'dynamic') return `socks5://127.0.0.1:${t.localPort}`
  if (t.mode === 'remote') return hostPort(t.remoteHost || '127.0.0.1', t.remotePort)
  return hostPort(t.listenHost || '127.0.0.1', t.localPort)
}

function addressLabel(t: TunnelInfo): string {
  if (t.mode === 'reverse-dynamic') return '服务器代理监听地址'
  if (t.mode === 'dynamic') return '代理监听地址'
  if (t.mode === 'remote') return '远端监听地址'
  return '本机监听地址'
}

function tunnelTarget(t: TunnelInfo): string {
  if (t.mode === 'reverse-dynamic') return '本机网络'
  if (t.mode === 'dynamic') return 'SSH 服务器网络'
  if (t.mode === 'remote') return hostPort('127.0.0.1', t.localPort)
  return hostPort(t.remoteHost, t.remotePort)
}

async function loadLocalIPv4() {
  if (loadingLocalIPv4.value) return
  loadingLocalIPv4.value = true
  localIPv4Error.value = false
  try {
    localIPv4.value = await sshService.listLocalIPv4()
    if (form.listenIP !== editing.value?.listenHost && !localIPv4.value.includes(form.listenIP)) {
      form.listenIP = ''
    }
  } catch {
    localIPv4Error.value = true
  } finally {
    loadingLocalIPv4.value = false
  }
}

function copyOptions(t: TunnelInfo): string[] {
  if (t.mode !== 'local' || t.listenHost !== '0.0.0.0') return []
  return [...localIPv4.value, '127.0.0.1'].map(ip => hostPort(ip, t.localPort))
}

async function copyAddress(t: TunnelInfo, address: string) {
  try {
    await ClipboardSetText(address)
    copiedId.value = t.id
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copiedId.value = ''
    }, 1600)
  } catch (e) {
    error.value = String(e)
  }
}

onMounted(async () => {
  if (!servers.servers.length) await servers.load()
  if (servers.servers.length && !form.serverId) form.serverId = servers.servers[0].id
  disposers.push(onTunnelStatus(onStatus))
  await refresh()
})

onBeforeUnmount(() => {
  disposers.forEach((d) => d())
  window.clearTimeout(copiedTimer)
})

const restartTunnel = (id: string) => runTunnelAction(id, sshService.restartTunnel)
const stopTunnel = (id: string) => runTunnelAction(id, sshService.stopTunnel)
const removeTunnel = (id: string) => runTunnelAction(id, sshService.removeTunnel)
return {servers, tunnels, loading, busy, error, showDialog, editing, copiedId, localIPv4, loadingLocalIPv4, localIPv4Error, pendingActions, form, listenIPOptions, modeDetails, currentModeDetail, isReverseDynamic, isFixedForward, hint, refresh, submit, openCreate, openEdit, closeDialog, restartTunnel, stopTunnel, removeTunnel, filteredTunnels, tunnelAddress, addressLabel, tunnelTarget, loadLocalIPv4, copyOptions, copyAddress}
}
