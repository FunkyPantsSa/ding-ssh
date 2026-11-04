<script lang="ts" setup>
import {Button, Field, Input} from '../ui'
import {useSettingsSecurity} from '../../composables/useSettingsSecurity'

const {security, form: secForm, error: secError, message: secMsg, busy: secBusy, enableMaster, disableMaster, changeMaster} = useSettingsSecurity()
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="security" aria-labelledby="settings-security-title">
    <div>
      <h3 id="settings-security-title" class="text-[18px] font-semibold text-white tracking-tight">安全</h3>
      <p class="text-[13px] text-mist mt-1.5">
        敏感字段 AES-256-GCM 加密；可选启动主密码（Argon2id）。
      </p>
    </div>

    <div class="rounded-xl border border-slate-700/60 bg-slate-900/60 px-5 py-4 text-xs space-y-2">
      <p class="text-slate-300">
        状态：
        <span :class="security?.unlocked ? 'text-emerald-400' : 'text-amber-400'">
          {{ security?.unlocked ? '已解锁' : '未解锁' }}
        </span>
      </p>
      <p class="text-slate-500">
        主密码：{{ security?.masterPasswordEnabled ? '已启用' : '未启用' }}
        · 钥匙串：{{ security?.keyringAvailable ? '可用' : '不可用（已回退本地密钥文件）' }}
      </p>
    </div>

    <div v-if="!security?.masterPasswordEnabled" class="neo">
      <div class="px-5 py-4 border-b border-slate-800/60">
        <p class="text-sm font-medium text-slate-200">启用启动主密码</p>
        <p class="text-xs text-slate-500 mt-1">开启后每次启动需输入密码才能访问服务器与凭证。</p>
      </div>
      <div class="px-5 py-4 space-y-3">
        <Field label="新主密码" html-for="settings-new-master">
          <Input id="settings-new-master" v-model="secForm.password" type="password" placeholder="新主密码" autocomplete="new-password" />
        </Field>
        <Field label="确认主密码" html-for="settings-confirm-master">
          <Input id="settings-confirm-master" v-model="secForm.confirm" type="password" placeholder="确认主密码" autocomplete="new-password" />
        </Field>
        <div class="flex items-center justify-between gap-3">
          <p v-if="secError" class="text-xs text-rose-400" role="alert">{{ secError }}</p>
          <p v-else-if="secMsg" class="text-xs text-emerald-400" role="status">{{ secMsg }}</p>
          <Button variant="primary" size="sm" class="ml-auto" :loading="secBusy" @click="enableMaster">{{ secBusy ? '处理中…' : '启用' }}</Button>
        </div>
      </div>
    </div>

    <template v-else>
      <div class="neo">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">更换主密码</p>
        </div>
        <div class="px-5 py-4 space-y-3">
          <Field label="当前主密码" html-for="settings-old-master">
            <Input id="settings-old-master" v-model="secForm.oldPassword" type="password" placeholder="当前主密码" autocomplete="current-password" />
          </Field>
          <Field label="新主密码" html-for="settings-change-master">
            <Input id="settings-change-master" v-model="secForm.newPassword" type="password" placeholder="新主密码" autocomplete="new-password" />
          </Field>
          <Field label="确认新主密码" html-for="settings-change-confirm">
            <Input id="settings-change-confirm" v-model="secForm.newConfirm" type="password" placeholder="确认新主密码" autocomplete="new-password" />
          </Field>
          <div class="flex justify-end">
            <Button variant="primary" size="sm" :loading="secBusy" @click="changeMaster">更换</Button>
          </div>
        </div>
      </div>

      <div class="neo">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">关闭主密码</p>
          <p class="text-xs text-slate-500 mt-1">关闭后主密钥改存系统钥匙串（失败时回退本地密钥文件）。</p>
        </div>
        <div class="px-5 py-4 space-y-3">
          <Field label="当前主密码" html-for="settings-disable-master">
            <Input id="settings-disable-master" v-model="secForm.oldPassword" type="password" placeholder="当前主密码" autocomplete="current-password" />
          </Field>
          <div class="flex items-center justify-between gap-3">
            <p v-if="secError" class="text-xs text-rose-400" role="alert">{{ secError }}</p>
            <p v-else-if="secMsg" class="text-xs text-emerald-400" role="status">{{ secMsg }}</p>
            <Button variant="danger" size="sm" class="ml-auto" :loading="secBusy" @click="disableMaster">关闭主密码</Button>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>
