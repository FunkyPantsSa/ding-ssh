<script setup lang="ts">
import {ref, watch} from 'vue'
import {Button, Dialog, Input, Select} from './ui'
import {useServersStore} from '../stores/servers'
import type {ServerNode} from '../types'
const show = defineModel<boolean>({required: true})
const servers = useServersStore()
interface Entry {alias: string; node: ServerNode; jumps: string[]; warnings: string[]}
const entries = ref<Entry[]>([])
const choices = ref<Record<string, string>>({})
const path = ref('')
const error = ref('')
const busy = ref(false)
const api = () => (window as any).go.main.App
function conflict(entry: Entry) { return servers.servers.find(s => s.name === entry.node.name || s.importAlias === entry.alias && s.importSource === entry.node.importSource) }
function diff(entry: Entry) {
 const old = conflict(entry); if (!old) return ''
 return ['host', 'port', 'user', 'authType', 'identityFiles', 'identityAgent', 'identitiesOnly', 'hostKeyAlias', 'knownHostsSources'].map(key => {
  const before = (old as any)[key]; const after = (entry.node as any)[key]
  return JSON.stringify(before) === JSON.stringify(after) ? '' : `${key}: ${JSON.stringify(before ?? '')} → ${JSON.stringify(after ?? '')}`
 }).filter(Boolean).join('\n') + `\n跳板机引用将按导入配置重建：${entry.jumps.join(' → ') || '直接连接'}`
}
async function preview() {
 busy.value = true; error.value = ''
 try { entries.value = await api().PreviewSSHConfig(path.value); choices.value = Object.fromEntries(entries.value.map(e => [e.alias, e.warnings.length || conflict(e) ? 'skip' : 'copy'])) }
 catch (e) { error.value = String(e); entries.value = [] }
 finally { busy.value = false }
}
async function choose() { try { const selected = await api().SelectSSHConfig(); if (selected) { path.value = selected; await preview() } } catch (e) { error.value = String(e) } }
async function save() {
 busy.value = true
 try { await api().ImportSSHConfig(path.value, entries.value.map(e => ({alias: e.alias, strategy: choices.value[e.alias]}))); await servers.load(); show.value = false }
 catch (e) { error.value = String(e) }
 finally { busy.value = false }
}
watch(show, value => { if (value) preview() })
</script>
<template>
 <Dialog v-model="show" title="导入 SSH Config">
  <div class="space-y-4">
   <p class="text-xs text-mist">生成配置快照，不修改源文件。带有不支持配置项的主机需要人工检查，不能直接导入。跳板依赖请一并选中。</p>
   <div class="flex flex-wrap items-center gap-2"><Input class="flex-1 basis-48" v-model="path" placeholder="留空使用 ~/.ssh/config" /><Button size="sm" @click="choose">选择文件</Button><Button size="sm" :disabled="busy" @click="preview">预览</Button></div>
   <p v-if="error" role="alert" class="text-sm text-rose-400">{{ error }}</p>
   <div v-for="entry in entries" :key="entry.alias" class="border-t border-slate-700/40 pt-3 space-y-2">
    <p class="text-sm font-medium">{{ entry.alias }} · {{ entry.node.host }}:{{ entry.node.port }} · {{ entry.node.user }}</p>
    <p class="text-xs text-mist">认证：{{ entry.node.identityFiles?.join(', ') || '系统 SSH Agent' }} · 跳板：{{ entry.jumps.join(' → ') || '直接连接' }}</p>
    <p v-for="warning in entry.warnings" :key="warning" class="text-xs text-amber-500">{{ warning }}</p>
    <pre v-if="conflict(entry)" class="text-xs whitespace-pre-wrap break-all">存在冲突，覆盖字段差异：{{ diff(entry) }}</pre>
    <Select v-model="choices[entry.alias]" :disabled="entry.warnings.length > 0"><option value="skip">跳过</option><option value="copy">{{ conflict(entry) ? '创建副本' : '导入' }}</option><option v-if="conflict(entry)" value="overwrite">覆盖（已查看差异）</option></Select>
   </div>
  </div>
  <template #footer><Button variant="ghost" @click="show = false">取消</Button><Button variant="primary" :disabled="busy || !entries.some(e => choices[e.alias] !== 'skip')" @click="save">导入选中主机</Button></template>
 </Dialog>
</template>
