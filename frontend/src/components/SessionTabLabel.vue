<script setup lang="ts">
import {computed} from 'vue'

const props = defineProps<{text: string}>()
// Keep the identifying end of a host/name visible when a narrow sidebar truncates it.
const parts = computed(() => {
  const chars = Array.from(props.text)
  return {prefix: chars.slice(0, -6).join(''), suffix: chars.slice(-6).join('')}
})
</script>

<template>
  <span class="nav-text side-tab-name" :aria-label="text">
    <span class="side-tab-name-prefix">{{ parts.prefix }}</span><span class="side-tab-name-suffix">{{ parts.suffix }}</span>
  </span>
</template>
