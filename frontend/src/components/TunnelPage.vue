<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {onTunnelStatus, sshService} from '../services/ssh'
import {useServersStore} from '../stores/servers'
import type {TunnelInfo, TunnelStatusEvent} from '../types'

const servers = useServersStore()
const tunnels = ref<TunnelInfo[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const form = reactive({
  serverId: '',
  name: '',
  localPort: 13306,
  remoteHost: '127.0.0.1',
  remotePort: 3306,
})
const disposers: Array<() => void> = []

const selectedServer = computed(() => servers.servers.find((s) => s.id === form.serverId))

// 切换服务器时自动填充隧道名称
watch(
  () => form.serverId,
  (id, old) => {
    const s = servers.servers.find((x) => x.id === id)
    if (!s) return
    if (!form.name || form.name === servers.servers.find((x) => x.id === old)?.name) {
      form.name = s.name
    }
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

async function create() {
  error.value = ''
  if (!form.serverId) {
    error.value = '请选择服务器'
    return
  }
  const node = selectedServer.value
  if (!node) return
  const localPort = Number(form.localPort)
  const remotePort = Number(form.remotePort)
  if (!Number.isInteger(localPort) || localPort < 1 || localPort > 65535) {
    error.value = '本地端口无效（1-65535）'
    return
  }
  if (!Number.isInteger(remotePort) || remotePort < 1 || remotePort > 65535) {
    error.value = '远程端口无效（1-65535）'
    return
  }
  busy.value = true
  try {
    await sshService.startTunnel(
      node,
      form.name.trim() || node.name,
      localPort,
      form.remoteHost.trim() || '127.0.0.1',
      remotePort,
    )
    await refresh()
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
  }
}

async function stop(id: string) {
  error.value = ''
  try {
    await sshService.stopTunnel(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

async function restart(id: string) {
  error.value = ''
  try {
    await sshService.restartTunnel(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

async function remove(id: string) {
  error.value = ''
  try {
    await sshService.removeTunnel(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

function onStatus(evt: TunnelStatusEvent) {
  const t = tunnels.value.find((x) => x.id === evt.id)
  if (!t) return
  t.status = evt.status
  if (evt.message) t.message = evt.message
}

const statusText: Record<string, string> = {
  running: '运行中',
  stopped: '已停止',
  error: '异常',
}

onMounted(async () => {
  if (!servers.servers.length) await servers.load()
  if (servers.servers.length && !form.serverId) form.serverId = servers.servers[0].id
  disposers.push(onTunnelStatus(onStatus))
  await refresh()
})

onBeforeUnmount(() => {
  disposers.forEach((d) => d())
})
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="max-w-3xl mx-auto px-8 py-8 space-y-6">
      <div>
        <h2 class="text-lg font-semibold text-slate-100">SSH 隧道</h2>
        <p class="text-xs text-slate-500 mt-1">
          基于已保存服务器建立本地端口转发，访问 <code class="text-sky-400">127.0.0.1:本地端口</code> 即相当于访问远程目标。
        </p>
      </div>

      <!-- 新建隧道 -->
      <div class="rounded-xl border border-slate-700/60 bg-slate-900/60">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">新建隧道</p>
          <p class="text-xs text-slate-500 mt-1">选择服务器作为跳板，指定本地端口与远程目标。</p>
        </div>

        <div class="px-5 py-4 space-y-4 text-[13px]">
          <div class="grid grid-cols-2 gap-4">
            <label class="block col-span-2">
              <span class="text-slate-400">跳板服务器</span>
              <select
                v-model="form.serverId"
                class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none focus:border-sky-500/60"
              >
                <option v-for="s in servers.servers" :key="s.id" :value="s.id">
                  {{ s.name }}（{{ s.user }}@{{ s.host }}:{{ s.port }}）
                </option>
              </select>
            </label>

            <label class="block">
              <span class="text-slate-400">隧道名称</span>
              <input
                v-model="form.name"
                class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none focus:border-sky-500/60"
                placeholder="自动填充为服务器名"
              />
            </label>

            <label class="block">
              <span class="text-slate-400">本地端口</span>
              <input
                v-model.number="form.localPort"
                type="number"
                min="1"
                max="65535"
                class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60"
              />
            </label>

            <label class="block">
              <span class="text-slate-400">远程目标主机</span>
              <input
                v-model="form.remoteHost"
                class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60"
                placeholder="127.0.0.1"
              />
            </label>

            <label class="block">
              <span class="text-slate-400">远程目标端口</span>
              <input
                v-model.number="form.remotePort"
                type="number"
                min="1"
                max="65535"
                class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60"
              />
            </label>
          </div>

          <div class="flex items-center justify-between gap-4">
            <p v-if="error" class="text-xs text-rose-400 break-all">{{ error }}</p>
            <p v-else class="text-[11px] text-slate-500">
              转发：127.0.0.1:{{ form.localPort || '?' }} → {{ form.remoteHost || '?' }}:{{ form.remotePort || '?' }}
            </p>
            <button
              class="ml-auto shrink-0 px-4 py-1.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 font-medium text-xs"
              :disabled="busy"
              @click="create"
            >
              {{ busy ? '创建中…' : '创建并启动' }}
            </button>
          </div>
        </div>
      </div>

      <!-- 隧道列表 -->
      <div class="rounded-xl border border-slate-700/60 bg-slate-900/60">
        <div class="px-5 py-4 border-b border-slate-800/60 flex items-center justify-between">
          <div>
            <p class="text-sm font-medium text-slate-200">隧道列表</p>
            <p class="text-xs text-slate-500 mt-1">运行中的隧道随应用退出自动关闭。</p>
          </div>
          <button class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-xs" @click="refresh">
            <Icon name="refresh" size="12" class="mr-1" /> 刷新
          </button>
        </div>

        <div class="px-5 py-4 space-y-2">
          <div v-if="!tunnels.length && !loading" class="text-xs text-slate-500 py-2">暂无隧道，在上方创建。</div>

          <div
            v-for="t in tunnels"
            :key="t.id"
            class="flex items-center gap-3 px-3 py-2.5 rounded-md bg-slate-800/50 border border-slate-700/40"
          >
            <span
              class="w-2 h-2 shrink-0 rounded-full"
              :class="t.status === 'running' ? 'bg-emerald-400' : t.status === 'error' ? 'bg-rose-400' : 'bg-slate-500'"
            ></span>

            <div class="min-w-0 flex-1">
              <p class="text-[13px] text-slate-200 truncate">{{ t.name }}</p>
              <p class="text-[11px] text-slate-500 truncate">
                {{ t.serverName }} · 127.0.0.1:{{ t.localPort }} → {{ t.remoteHost }}:{{ t.remotePort }}
              </p>
              <p v-if="t.status === 'error' && t.message" class="text-[11px] text-rose-400 break-all mt-0.5">{{ t.message }}</p>
            </div>

            <span
              class="shrink-0 px-2 py-0.5 rounded-full text-[10px]"
              :class="
                t.status === 'running'
                  ? 'bg-emerald-500/15 text-emerald-300'
                  : t.status === 'error'
                    ? 'bg-rose-500/15 text-rose-300'
                    : 'bg-slate-700/50 text-slate-400'
              "
            >
              {{ statusText[t.status] }}
            </span>

            <div class="shrink-0 flex items-center gap-1">
              <button
                v-if="t.status === 'running'"
                class="px-2 py-1 rounded bg-amber-500/70 hover:bg-amber-400 text-slate-900 text-xs"
                title="停止隧道"
                @click="stop(t.id)"
              >
                停止
              </button>
              <button
                v-else
                class="px-2 py-1 rounded bg-sky-500/70 hover:bg-sky-400 text-slate-900 text-xs"
                title="重新启动隧道"
                @click="restart(t.id)"
              >
                启动
              </button>
              <button
                class="px-2 py-1 rounded bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-xs"
                title="移除隧道"
                @click="remove(t.id)"
              >
                删除
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
