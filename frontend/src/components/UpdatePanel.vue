<script setup lang="ts">
import {onMounted, onBeforeUnmount} from 'vue'
import {updateVisible, updateInfo, updateBusy, updateReady, updateMessage, updateProgress, dismissUpdate, downloadUpdate, installUpdateNow, startUpdateChecks} from '../services/updates'
import {Button} from './ui'
let off: (() => void) | undefined
let disposed = false
onMounted(async () => { const cleanup = await startUpdateChecks(); if (disposed) cleanup(); else off = cleanup })
onBeforeUnmount(() => { disposed = true; off?.() })
</script>
<template>
  <div v-if="updateVisible" class="update-panel fixed bottom-5 right-5 z-[100] w-96 max-w-[calc(100vw-2.5rem)] p-5 text-slate-200" role="dialog" aria-label="应用更新">
    <div class="flex items-center justify-between"><strong>发现新版本</strong><button aria-label="关闭更新提示" @click="dismissUpdate">×</button></div>
    <p v-if="updateInfo" class="mt-3 text-xs break-all">{{ updateInfo.manifest.channel }} · {{ updateInfo.manifest.commit.slice(0, 12) }} · {{ new Date(updateInfo.manifest.builtAt).toLocaleString() }}</p>
    <p class="mt-3 text-sm" aria-live="polite">{{ updateMessage }} {{ updateBusy ? updateProgress : '' }}</p>
    <p class="mt-3 text-xs text-slate-400">下载遵循系统代理。安装会关闭 SSH 会话和文件传输，请先完成当前操作。Windows 安装目录权限不足时会请求管理员授权，取消授权将保留原版本。</p>
    <div class="mt-4 flex gap-3">
      <Button v-if="!updateReady" size="sm" :disabled="updateBusy" @click="downloadUpdate(true)">立即更新</Button>
      <Button v-if="!updateReady" variant="ghost" size="sm" :disabled="updateBusy" @click="downloadUpdate(false)">退出时更新</Button>
      <Button v-if="updateReady" size="sm" @click="installUpdateNow">立即重启安装</Button>
    </div>
  </div>
</template>
