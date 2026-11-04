<script setup lang="ts">
import Icon from '../Icon.vue'
import {Button, Input} from '../ui'

defineProps<{
  modelValue: string
  total: number
  online: number
  testing: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  refresh: []
  testAll: []
  create: []
}>()
</script>

<template>
  <div class="server-toolbar flex items-center gap-2 shrink-0" data-testid="server-toolbar">
    <div class="search server-search">
      <Icon name="search" :size="14" extra-class="search-ico" />
      <Input
        :model-value="modelValue"
        type="text"
        class="server-search-input"
        placeholder="搜索名称 / 主机 / 分组…"
        aria-label="搜索服务器"
        data-testid="server-search"
        @update:model-value="emit('update:modelValue', String($event))"
      />
    </div>
    <div class="ml-auto flex items-center gap-2">
      <span class="chip" data-testid="server-online-count"><span class="dot"></span>{{ online }}/{{ total }} 台在线</span>
      <Button
        variant="ghost"
        size="sm"
        :title="testing ? '正在测试全部服务器…' : '测试全部服务器在线状态'"
        :disabled="testing || !total"
        data-testid="server-test-all"
        @click="emit('testAll')"
      >
        <Icon name="activity" :size="14" :extra-class="testing ? 'animate-spin' : ''" />测试全部
      </Button>
      <Button variant="ghost" size="sm" title="刷新" aria-label="刷新" data-testid="server-refresh" @click="emit('refresh')"><Icon name="refresh" :size="14" />刷新</Button>
      <Button variant="primary" size="sm" data-testid="server-create" @click="emit('create')"><Icon name="plus" :size="14" />新建服务器</Button>
    </div>
  </div>
</template>

<style scoped>
.server-search { max-width: 340px; }
.server-search-input { height: 30px; padding-left: 32px; }
</style>
