<script setup lang="ts">
import {Button, Field, Input} from '../ui'

const props = defineProps<{ exportPass: string; exportPassConfirm: string; exportError: string; exportMsg: string; exportBusy: boolean; importPass: string; importError: string; importMsg: string; importBusy: boolean; overwrite: boolean }>()
const emit = defineEmits<{ 'update:exportPass': [value: string]; 'update:exportPassConfirm': [value: string]; 'update:importPass': [value: string]; 'update:overwrite': [value: boolean]; export: []; import: [] }>()
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" aria-labelledby="settings-import-export-title">
    <div>
      <h3 id="settings-import-export-title" class="text-[18px] font-semibold text-white tracking-tight">导入导出</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">使用加密的 .dingpack 在设备间迁移服务器、凭证与设置。</p>
    </div>
    <div class="grid gap-4 md:grid-cols-2">
      <div class="neo">
        <div class="px-5 py-4 border-b border-slate-800/60"><p class="text-sm font-medium text-slate-200">导出 .dingpack</p></div>
        <div class="px-5 py-4 space-y-3">
          <Field label="导出密码" html-for="settings-export-password">
            <Input id="settings-export-password" :model-value="props.exportPass" type="password" placeholder="导出密码" data-testid="settings-export-password" @update:model-value="emit('update:exportPass', String($event))" />
          </Field>
          <Field label="确认导出密码" html-for="settings-export-password-confirm">
            <Input id="settings-export-password-confirm" :model-value="props.exportPassConfirm" type="password" placeholder="再次输入导出密码" @update:model-value="emit('update:exportPassConfirm', String($event))" />
          </Field>
          <div class="flex items-center justify-between gap-3">
            <p v-if="props.exportError" class="text-xs text-rose-400 break-all" role="alert">{{ props.exportError }}</p>
            <p v-else-if="props.exportMsg" class="text-xs text-emerald-400 break-all" role="status">{{ props.exportMsg }}</p>
            <Button variant="primary" size="sm" class="ml-auto" :loading="props.exportBusy" data-testid="settings-export" @click="emit('export')">{{ props.exportBusy ? '处理中…' : '导出…' }}</Button>
          </div>
        </div>
      </div>
      <div class="neo">
        <div class="px-5 py-4 border-b border-slate-800/60"><p class="text-sm font-medium text-slate-200">导入 .dingpack</p></div>
        <div class="px-5 py-4 space-y-3">
          <Field label="导入密码" html-for="settings-import-password">
            <Input id="settings-import-password" :model-value="props.importPass" type="password" placeholder="导入密码" data-testid="settings-import-password" @update:model-value="emit('update:importPass', String($event))" />
          </Field>
          <label class="flex items-center gap-2 text-xs text-slate-400">
            <input :checked="props.overwrite" type="checkbox" class="rounded border-slate-600" @change="emit('update:overwrite', ($event.target as HTMLInputElement).checked)" />
            同 ID 覆盖已有服务器 / 凭证
          </label>
          <div class="flex items-center justify-between gap-3">
            <p v-if="props.importError" class="text-xs text-rose-400 break-all" role="alert">{{ props.importError }}</p>
            <p v-else-if="props.importMsg" class="text-xs text-emerald-400 break-all" role="status">{{ props.importMsg }}</p>
            <Button variant="primary" size="sm" class="ml-auto" :loading="props.importBusy" data-testid="settings-import" @click="emit('import')">{{ props.importBusy ? '处理中…' : '导入…' }}</Button>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
