<script setup lang="ts">
import {onMounted, ref} from 'vue'
import {CreateDebugClient, ListDebugClients, RevokeDebugClient} from '../../../wailsjs/go/main/App'
import {Button, Input, Textarea} from '../ui'
import {copyText} from '../../utils/clipboard'
import {errorText, t} from '../../i18n'

type Client = {id: string; name: string; capabilities: string[]; sessionIds: string[]; serverIds: string[]; expiresAt: number}
const clients = ref<Client[]>([])
const name = ref('')
const targets = ref('')
const servers = ref('')
const readPaths = ref('')
const writePaths = ref('')
const selected = ref(['read', 'terminal.read', 'fs.remote.read'])
const choices = ['read', 'terminal.read', 'fs.remote.read', 'session.connect', 'session.disconnect', 'credentials.use', 'terminal.input', 'fs.remote.write', 'transfer.cancel']
const busy = ref(false)
const error = ref('')
const issuedToken = ref('')
const lines = (s: string) => [...new Set(s.split(/[\n,]/).map(v => v.trim()).filter(Boolean))]
async function refresh() {
  try { clients.value = await ListDebugClients() as Client[] } catch (e) { error.value = errorText(e) }
}
async function create() {
  busy.value = true
  error.value = ''
  issuedToken.value = ''
  try {
    if (!name.value.trim() || (!lines(targets.value).length && !lines(servers.value).length)) {
      throw new Error(t('请填写客户端名称和至少一个服务器或会话 ID'))
    }
    const result = await CreateDebugClient(JSON.stringify({
      name: name.value.trim(), capabilities: selected.value,
      serverIds: lines(servers.value), sessionIds: lines(targets.value),
      readPaths: lines(readPaths.value), writePaths: lines(writePaths.value),
      allTargets: false, expiresAt: Date.now() + 60 * 60 * 1000,
    }))
    issuedToken.value = result.token
    await refresh()
  } catch (e) { error.value = errorText(e) } finally { busy.value = false }
}
async function revoke(id: string) {
  busy.value = true
  try { await RevokeDebugClient(id); issuedToken.value = ''; await refresh() }
  catch (e) { error.value = errorText(e) } finally { busy.value = false }
}
onMounted(refresh)
</script>

<template>
  <div class="neo px-5 py-4 space-y-3">
    <p class="text-sm font-medium text-slate-200">{{ t('客户端授权') }}</p>
    <p class="text-xs text-slate-500 leading-relaxed">{{ t('为自动化创建独立 token，限定目标、路径和能力。授权有效一小时，应用重启后失效；受限客户端不能导出凭据、执行本机命令或控制任意界面。全局能力开关仍然生效。') }}</p>
    <Input v-model="name" :aria-label="t('客户端名称')" :placeholder="t('客户端名称')" />
    <Input v-model="servers" :aria-label="t('服务器 ID')" :placeholder="t('服务器 ID，逗号分隔')" />
    <Input v-model="targets" :aria-label="t('会话 ID')" :placeholder="t('会话 ID，逗号分隔')" />
    <p class="text-xs text-slate-500">{{ t('服务器和会话分别授权。连接新会话后，可使用它的 ID 创建新的会话授权。') }}</p>
    <div class="grid grid-cols-2 gap-2 text-xs text-slate-300">
      <label v-for="cap in choices" :key="cap" class="flex items-center gap-2">
        <input v-model="selected" type="checkbox" :value="cap" :disabled="busy" />
        <span class="font-mono">{{ cap }}</span>
      </label>
    </div>
    <Textarea v-model="readPaths" :rows="2" :aria-label="t('客户端可读路径')" :placeholder="t('可读绝对路径，一行一个；留空禁止文件读取')" />
    <Textarea v-model="writePaths" :rows="2" :aria-label="t('客户端可写路径')" :placeholder="t('可写绝对路径，一行一个；同时受全局写白名单约束')" />
    <Button size="sm" :disabled="busy" @click="create">{{ t('创建授权（需系统确认）') }}</Button>
    <div v-if="issuedToken" class="rounded border border-amber-500/30 p-3 space-y-2">
      <p class="text-xs text-amber-300">{{ t('token 仅显示本次，请妥善保存。') }}</p>
      <code class="block text-xs break-all select-all">{{ issuedToken }}</code>
      <Button size="sm" @click="copyText(issuedToken)">{{ t('复制 token') }}</Button>
      <Button size="sm" variant="ghost" @click="issuedToken = ''">{{ t('隐藏') }}</Button>
    </div>
    <div v-for="client in clients" :key="client.id" class="border-t border-slate-800/60 pt-3 flex items-start justify-between gap-3">
      <div class="text-xs min-w-0">
        <p class="text-slate-200">{{ client.name || client.id }}</p>
        <p class="font-mono text-slate-500 break-all">{{ client.id }}</p>
        <p class="text-slate-500">{{ t('到期：') }}{{ new Date(client.expiresAt).toLocaleString() }}</p>
        <p class="text-slate-500 break-all">{{ (client.capabilities || []).join(' · ') }}</p>
      </div>
      <Button size="sm" variant="danger" :disabled="busy" @click="revoke(client.id)">{{ t('撤销') }}</Button>
    </div>
    <p v-if="error" class="text-xs text-rose-400 break-all" role="alert">{{ error }}</p>
  </div>
</template>
