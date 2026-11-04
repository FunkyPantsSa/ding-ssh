<script setup lang="ts">
import {HOTKEYS, hotkeyLabel} from '../../hotkeys'
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" aria-labelledby="settings-hotkeys-title">
    <div>
      <h3 id="settings-hotkeys-title" class="text-[18px] font-semibold text-white tracking-tight">快捷键</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">快捷键由应用注册表生成；输入法组合中不会触发，表单输入时仅保留命令面板与 Esc。</p>
    </div>
    <div class="neo overflow-hidden">
      <div v-for="hotkey in HOTKEYS" :key="hotkey.id" class="flex items-center justify-between gap-4 px-5 py-3 border-b border-slate-800/60 last:border-b-0" :data-testid="`hotkey-${hotkey.id}`">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">{{ hotkey.label }}</p>
          <p class="text-[11px] uppercase tracking-wider text-slate-500 mt-0.5">{{ hotkey.scope }}</p>
        </div>
        <kbd class="kbd shrink-0">{{ hotkeyLabel(hotkey.keys) }}</kbd>
      </div>
    </div>
  </section>
</template>
