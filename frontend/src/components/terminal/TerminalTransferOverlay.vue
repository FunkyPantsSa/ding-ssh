<script setup lang="ts">
import type { ZmodemProgress } from '../../services/zmodem'
defineProps<{webglFallbackToast: string; zmodemMsg: string; zmodemProgress: ZmodemProgress | null}>()
</script>

<template>
  <div v-if="webglFallbackToast || zmodemMsg || (zmodemProgress && !zmodemProgress.done)" class="absolute left-3 right-3 bottom-3 z-40 pointer-events-none flex flex-col gap-2 items-start">
    <div v-if="webglFallbackToast" class="toast neo" style="min-width:auto">{{ webglFallbackToast }}</div>
    <div v-if="zmodemMsg" class="toast neo ok" style="min-width:auto">{{ zmodemMsg }}</div>
    <div v-if="zmodemProgress && !zmodemProgress.done" class="neo w-full max-w-sm px-3 py-2 text-xs"><div class="flex justify-between gap-2 mb-1"><span class="truncate">{{ zmodemProgress.direction === 'upload' ? '上传' : '下载' }} · {{ zmodemProgress.name }}</span><span class="text-signal shrink-0">{{ zmodemProgress.total > 0 ? Math.min(100, Math.round((zmodemProgress.transferred / zmodemProgress.total) * 100)) : 0 }}%</span></div><div class="prog"><i :style="{width: zmodemProgress.total > 0 ? `${Math.min(100, (zmodemProgress.transferred / zmodemProgress.total) * 100)}%` : '10%'}"></i></div></div>
  </div>
</template>
