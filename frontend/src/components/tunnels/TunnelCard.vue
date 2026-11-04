<script setup lang="ts">
import Icon from '@/components/Icon.vue'
import type {TunnelInfo} from '@/types'
import {Button} from '@/components/ui'

defineProps<{
  tunnel: TunnelInfo
  address: string
  addressLabel: string
  path: string
  copied: boolean
}>()

const emit = defineEmits<{copy: []; start: []; stop: []; restart: []; edit: []; remove: []}>()
</script>

<template>
  <article class="neo neo-hover p-5 flex flex-col gap-3">
    <div class="flex items-start justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--foreground)] min-w-0 truncate">{{ tunnel.name }}</h3>
      <span class="badge" :class="tunnel.status === 'running' ? 'live' : tunnel.status === 'error' ? 'err' : 'stop'">{{ tunnel.status === 'running' ? 'LIVE' : tunnel.status === 'error' ? 'ERROR' : 'STOPPED' }}</span>
    </div>
    <Button variant="ghost" class="neo-flat w-full !justify-start h-auto px-3 py-2" :title="`复制 ${address}`" @click="emit('copy')">
      <span class="min-w-0 flex-1 text-left"><span class="block text-[11px] text-mist">{{ addressLabel }}</span><span class="block font-mono text-[13px] text-[var(--foreground)] truncate">{{ address }}</span></span>
      <Icon :name="copied ? 'check' : 'copy'" :size="15" />
      <span class="text-[11px] shrink-0" :class="copied ? 'text-[var(--primary-hover)]' : 'text-mist'">{{ copied ? '已复制' : '复制' }}</span>
    </Button>
    <div class="text-[12px] text-mist leading-relaxed">{{ path }}</div>
    <p v-if="tunnel.status === 'error' && tunnel.message" class="text-[12px] text-danger break-all">{{ tunnel.message }}</p>
    <div class="flex flex-wrap gap-1.5">
      <Button v-if="tunnel.status === 'running'" variant="ghost" size="sm" @click="emit('stop')">停止</Button>
      <Button v-else variant="primary" size="sm" @click="emit('start')">启动</Button>
      <Button v-if="tunnel.status === 'running'" variant="ghost" size="sm" @click="emit('restart')">重启</Button>
      <Button variant="ghost" size="sm" @click="emit('edit')">编辑</Button>
      <Button variant="ghost" size="sm" @click="emit('remove')">删除</Button>
    </div>
  </article>
</template>
