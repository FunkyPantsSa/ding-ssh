<script setup lang="ts">
import {onMounted, ref} from 'vue'
import {Button} from '../ui'
interface TrustRecord {line: number; hosts: string; keyType: string; fingerprint: string; source: string; readOnly: boolean}
const records = ref<TrustRecord[]>([])
const error = ref('')
const api = () => (window as any).go.main.App
async function refresh() {
  try { records.value = await api().GetHostTrust(); error.value = '' }
  catch (e) { error.value = String(e) }
}
async function remove(record: TrustRecord) {
  try { await api().DeleteHostTrust(record.line, record.fingerprint); await refresh() }
  catch (e) { error.value = String(e) }
}
function copy(value: string) { navigator.clipboard.writeText(value).catch(e => error.value = String(e)) }
onMounted(refresh)
</script>
<template>
  <section class="neo p-5 space-y-3" aria-label="SSH 主机信任管理">
    <div class="flex justify-between items-center"><h4 class="text-sm font-semibold">SSH 主机信任管理</h4><Button size="sm" @click="refresh">刷新</Button></div>
    <p class="text-xs text-mist">主机密钥变化会阻止连接。合法轮换时，先通过可信渠道核对新指纹，再删除应用记录并重新连接确认。OpenSSH 来源保持只读。</p>
    <p v-if="error" role="alert" class="text-sm text-rose-400">{{ error }}</p>
    <p v-if="!records.length" class="text-xs text-mist">暂无已信任主机</p>
    <div v-for="record in records" :key="record.source + ':' + record.line" class="border-t border-slate-700/40 pt-3 space-y-1 text-xs">
      <p>{{ record.hosts }} · {{ record.keyType }}</p>
      <p class="break-all select-text">{{ record.fingerprint }}</p>
      <p class="text-mist break-all">{{ record.source }}{{ record.readOnly ? '（只读）' : '' }}</p>
      <Button size="sm" variant="ghost" @click="copy(record.fingerprint)">复制指纹</Button>
      <Button v-if="!record.readOnly" size="sm" variant="ghost" @click="remove(record)">删除信任记录</Button>
    </div>
  </section>
</template>
