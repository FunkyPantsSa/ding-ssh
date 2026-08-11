<script lang="ts" setup>
defineProps<{name: string; size?: number; class?: string}>()
</script>

<template>
  <svg
    :width="size ?? 16"
    :height="size ?? 16"
    viewBox="0 0 16 16"
    fill="currentColor"
    :class="class"
    aria-hidden="true"
  >
    <!-- 终端 -->
    <template v-if="name === 'terminal'">
      <path d="M3.5 3.5L7 7l-3.5 3.5M8 11h4" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 隧道 -->
    <template v-else-if="name === 'tunnel'">
      <path d="M1 8h4l2 5 2-10 2 5h4" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 设置 -->
    <template v-else-if="name === 'settings'">
      <circle cx="8" cy="8" r="2" stroke="currentColor" stroke-width="1.2" fill="none"/>
      <path d="M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.5 1.5M11.5 11.5L13 13M3 13l1.5-1.5M11.5 4.5L13 3" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>
    </template>
    <!-- 文件夹 -->
    <template v-else-if="name === 'folder'">
      <path d="M1 3.5v9a1 1 0 001 1h12a1 1 0 001-1V5a1 1 0 00-1-1H8L6.5 2.5H2a1 1 0 00-1 1z" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linejoin="round"/>
    </template>
    <!-- 文件 -->
    <template v-else-if="name === 'file'">
      <path d="M3 1.5h6l3 3v10a1 1 0 01-1 1H3a1 1 0 01-1-1V2.5a1 1 0 011-1z" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linejoin="round"/>
      <path d="M9 1.5v3h3" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linejoin="round"/>
    </template>
    <!-- 上传 -->
    <template v-else-if="name === 'upload'">
      <path d="M8 1v10M4 5l4-4 4 4M1 12v2a1 1 0 001 1h12a1 1 0 001-1v-2" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 下载 -->
    <template v-else-if="name === 'download'">
      <path d="M8 12V2M4 8l4 4 4-4M1 13v1a1 1 0 001 1h12a1 1 0 001-1v-1" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 关闭 × -->
    <template v-else-if="name === 'close'">
      <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </template>
    <!-- 加号 -->
    <template v-else-if="name === 'plus'">
      <path d="M8 2v12M2 8h12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </template>
    <!-- 键 -->
    <template v-else-if="name === 'key'">
      <circle cx="5" cy="11" r="2.5" stroke="currentColor" stroke-width="1.2" fill="none"/>
      <path d="M7 11l4-4 1.5 1.5M12 6.5l1 1" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>
    </template>
    <!-- 刷新 -->
    <template v-else-if="name === 'refresh'">
      <path d="M13 3A7 7 0 113 8" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round"/>
      <path d="M13 3V.5M13 3h-2.5" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 上一级 -->
    <template v-else-if="name === 'up'">
      <path d="M8 12V4M4 8l4-4 4 4" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 齿轮 -->
    <template v-else-if="name === 'gear'">
      <path d="M8 4.5V3M8 13v-1.5M4.5 8H3M13 8h-1.5M5.5 5.5L4.5 4.5M11.5 11.5l-1-1M5.5 10.5L4.5 11.5M11.5 4.5l-1 1" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>
      <circle cx="8" cy="8" r="2" stroke="currentColor" stroke-width="1.2" fill="none"/>
    </template>
    <!-- 搜索 -->
    <template v-else-if="name === 'search'">
      <circle cx="6.5" cy="6.5" r="3.5" stroke="currentColor" stroke-width="1.2" fill="none"/>
      <path d="M9.5 9.5L13 13" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>
    </template>
    <!-- 当前无匹配时显示通用图标 -->
    <template v-else>
      <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.2" fill="none"/>
    </template>
  </svg>
</template>
