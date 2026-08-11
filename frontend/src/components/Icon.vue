<script lang="ts" setup>
defineProps<{name: string; size?: number; extraClass?: string}>()
</script>

<template>
  <svg
    :width="size ?? 16"
    :height="size ?? 16"
    viewBox="0 0 16 16"
    fill="currentColor"
    :class="extraClass"
    aria-hidden="true"
  >
    <!-- 终端 -->
    <template v-if="name === 'terminal'">
      <path d="M2 4l5 4-5 4M9 11h5" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 隧道 -->
    <template v-else-if="name === 'tunnel'">
      <path d="M1 8h3l1.5 3 2.5-6 2.5 6 1.5-3h3" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 设置 -->
    <template v-else-if="name === 'settings'">
      <circle cx="8" cy="8" r="2.5" stroke="currentColor" stroke-width="1.5" fill="none"/>
      <path d="M8 1v2M8 13v2M1 8h2M13 8h2M3.5 3.5l1.5 1.5M11 11l1.5 1.5M3.5 12.5L5 11M11 5l1.5-1.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </template>
    <!-- 文件夹 -->
    <template v-else-if="name === 'folder'">
      <path d="M1.5 4v9a1 1 0 001 1h11a1 1 0 001-1V6a1 1 0 00-1-1H8.5L7 3.5H2.5a1 1 0 00-1 1z" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linejoin="round"/>
    </template>
    <!-- 文件 -->
    <template v-else-if="name === 'file'">
      <path d="M3 1.5h5.5l4 4V13a1.5 1.5 0 01-1.5 1.5h-8A1.5 1.5 0 011.5 13V3A1.5 1.5 0 013 1.5z" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linejoin="round"/>
      <path d="M8.5 1.5v4h4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linejoin="round"/>
    </template>
    <!-- 上传 -->
    <template v-else-if="name === 'upload'">
      <path d="M8 1v10M4 5l4-4 4 4M1.5 12v1.5a1 1 0 001 1h11a1 1 0 001-1V12" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 下载 -->
    <template v-else-if="name === 'download'">
      <path d="M8 12V2M4 8l4 4 4-4M1.5 13v1.5a1 1 0 001 1h11a1 1 0 001-1V13" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 关闭 × -->
    <template v-else-if="name === 'close'">
      <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </template>
    <!-- 加号 -->
    <template v-else-if="name === 'plus'">
      <path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </template>
    <!-- 键 -->
    <template v-else-if="name === 'key'">
      <circle cx="5.5" cy="10.5" r="2.5" stroke="currentColor" stroke-width="1.5" fill="none"/>
      <path d="M7.5 10.5l3.5-3.5 1.5 1.5M12.5 6.5l1 1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 刷新 -->
    <template v-else-if="name === 'refresh'">
      <path d="M13.5 3A7.5 7.5 0 103 8" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round"/>
      <path d="M13.5 3V.5M13.5 3H11" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 上一级 -->
    <template v-else-if="name === 'up'">
      <path d="M8 13V3M4 7l4-4 4 4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
    </template>
    <!-- 齿轮 -->
    <template v-else-if="name === 'gear'">
      <path d="M8 5V3M8 13v-2M5 8H3M13 8h-2M5.5 5.5L4 4M12 12l-1.5-1.5M5.5 10.5L4 12M12 4l-1.5 1.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
      <circle cx="8" cy="8" r="2.5" stroke="currentColor" stroke-width="1.5" fill="none"/>
    </template>
    <!-- 搜索 -->
    <template v-else-if="name === 'search'">
      <circle cx="6.5" cy="6.5" r="3.5" stroke="currentColor" stroke-width="1.5" fill="none"/>
      <path d="M9.5 9.5L14 14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </template>
    <!-- 当前无匹配时显示通用图标 -->
    <template v-else>
      <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.5" fill="none"/>
    </template>
  </svg>
</template>
