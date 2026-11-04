<script setup lang="ts">
import Icon from '@/components/Icon.vue'

export type SettingsNavItem = {
  key: string
  label: string
  icon: string
}

defineProps<{
  items: readonly SettingsNavItem[]
  modelValue: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()
</script>

<template>
  <nav class="set-nav" data-ui="settings-nav" aria-label="设置分类">
    <button
      v-for="item in items"
      :key="item.key"
      :class="modelValue === item.key ? 'active' : ''"
      :data-section="item.key"
      :aria-current="modelValue === item.key ? 'page' : undefined"
      @click="emit('update:modelValue', item.key)"
    >
      <Icon :name="item.icon" :size="14" />
      {{ item.label }}
    </button>
  </nav>
</template>
