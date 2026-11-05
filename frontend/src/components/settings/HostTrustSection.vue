<script setup lang="ts">
import {computed, onMounted, ref, watch} from 'vue'
import {Button, Input} from '../ui'
import {ClipboardSetText} from '../../../wailsjs/runtime/runtime'
interface TrustRecord {line: number; hosts: string; keyType: string; fingerprint: string; source: string; readOnly: boolean}
const records = ref<TrustRecord[]>([])
const error = ref('')
const keyword = ref('')
const page = ref(1)
const filtered = computed(() => records.value.filter(record => `${record.hosts} ${record.fingerprint} ${record.keyType}`.toLowerCase().includes(keyword.value.trim().toLowerCase())))
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / 10)))
const visible = computed(() => filtered.value.slice((page.value - 1) * 10, page.value * 10))
watch(keyword, () => page.value = 1)
watch(pages, value => page.value = Math.min(page.value, value))
const api = () => (window as any).go.main.App
async function refresh() {
  try { records.value = await api().GetHostTrust(); error.value = '' }
  catch (e) { error.value = String(e) }
}
async function remove(record: TrustRecord) {
  try { await api().DeleteHostTrust(record.line, record.fingerprint); await refresh() }
  catch (e) { error.value = String(e) }
}
function copy(value: string) { ClipboardSetText(value).catch(e => error.value = String(e)) }
onMounted(refresh)
</script>
<template>
  <section class="neo p-5 space-y-3" aria-label="SSH 主机信任管理">
    <div class="flex justify-between items-center"><h4 class="text-sm font-semibold">SSH 主机信任管理</h4><Button size="sm" @click="refresh">刷新</Button></div>
    <p class="text-xs text-mist">主机密钥变化会阻止连接。合法轮换时，先通过可信渠道核对新指纹，再删除应用记录并重新连接确认。OpenSSH 来源保持只读。</p>
    <Input v-model="keyword" placeholder="搜索 IP、主机名或指纹" aria-label="搜索已信任主机" />
    <p v-if="error" role="alert" class="text-sm text-rose-400">{{ error }}</p>
    <p v-if="records.length && !filtered.length" class="text-xs text-mist">没有匹配的主机</p>
    <p v-if="!records.length" class="text-xs text-mist">暂无已信任主机</p>
    <div class="max-h-96 overflow-y-auto space-y-3">
    <div v-for="record in visible" :key="record.source + ':' + record.line" class="border-t border-slate-700/40 pt-3 space-y-1 text-xs">
      <p>{{ record.hosts }} · {{ record.keyType }}</p>
      <p class="break-all select-text">{{ record.fingerprint }}</p>
      <p class="text-mist break-all">{{ record.source }}{{ record.readOnly ? '（只读）' : '' }}</p>
      <Button size="sm" variant="ghost" @click="copy(record.fingerprint)">复制指纹</Button>
      <Button v-if="!record.readOnly" size="sm" variant="ghost" @click="remove(record)">删除信任记录</Button>
    </div>
    </div>
    <div class="flex items-center justify-between gap-2 text-xs"><span>共 {{ filtered.length }} 条 · {{ page }}/{{ pages }} 页</span><div class="flex gap-2"><Button size="sm" :disabled="page <= 1" @click="page--">上一页</Button><Button size="sm" :disabled="page >= pages" @click="page++">下一页</Button></div></div>
  </section>
</template>
