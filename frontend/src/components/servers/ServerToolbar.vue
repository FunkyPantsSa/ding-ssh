<script setup lang="ts">
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

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
  importConfig: []
  create: []
}>()
</script>

<template>
  <div class="server-toolbar flex flex-wrap items-center gap-2 shrink-0" data-testid="server-toolbar">
    <div class="search server-search">
      <Icon name="search" :size="14" extra-class="search-ico" />
      <Input
        :model-value="modelValue"
        type="text"
        class="server-search-input"
        :placeholder="$t('搜索名称 / 主机 / 分组…')"
        :aria-label="$t('搜索服务器')"
        data-testid="server-search"
        @update:model-value="emit('update:modelValue', String($event))"
      />
    </div>
    <div class="ml-auto flex flex-wrap items-center gap-2">
      <span class="chip" data-testid="server-online-count"><span class="dot"></span> {{ online }}/{{ total }} {{ $t('台在线') }}</span>
      <Button
        variant="ghost"
        size="sm"
        :title="testing ? t('正在测试全部服务器…') : t('测试全部服务器在线状态')"
        :disabled="testing || !total"
        data-testid="server-test-all"
        @click="emit('testAll')"
      >
        <Icon name="activity" :size="14" :extra-class="testing ? 'animate-spin' : ''" />{{ $t('测试全部') }}</Button>
      <Button variant="ghost" size="sm" :title="$t('刷新')" :aria-label="$t('刷新')" data-testid="server-refresh" @click="emit('refresh')"><Icon name="refresh" :size="14" />{{ $t('刷新') }}</Button>
      <Button variant="secondary" size="sm" @click="emit('importConfig')">{{ $t('导入 SSH Config') }}</Button>
      <Button variant="primary" size="sm" data-testid="server-create" @click="emit('create')"><Icon name="plus" :size="14" />{{ $t('新建服务器') }}</Button>
    </div>
  </div>
</template>

<style scoped>
.server-search { max-width: 340px; }
.server-search-input { height: 30px; padding-left: 32px; }
</style>
