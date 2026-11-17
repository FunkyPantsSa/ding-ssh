<script lang="ts" setup>
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {Button, Field, Input} from '../ui'
import HostTrustSection from './HostTrustSection.vue'
import {useSettingsSecurity} from '../../composables/useSettingsSecurity'

const {security, loading, statusError, refresh, form: secForm, error: secError, message: secMsg, busy: secBusy, enableMaster, disableMaster, changeMaster} = useSettingsSecurity()
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="security" aria-labelledby="settings-security-title">
    <div>
      <h3 id="settings-security-title" class="text-[18px] font-semibold text-white tracking-tight">{{ $t('安全') }}</h3>
      <p class="text-[13px] text-mist mt-1.5">{{ $t('敏感字段 AES-256-GCM 加密；可选启动主密码（Argon2id）。') }}</p>
    </div>

    <p v-if="loading" class="text-sm text-mist" role="status">{{ $t('正在读取安全状态…') }}</p>
    <div v-else-if="statusError" role="alert" class="text-sm space-y-2">
      <p>{{ $t('无法读取安全状态：') }}{{ statusError }}</p>
      <Button variant="ghost" size="sm" @click="refresh">{{ $t('重试') }}</Button>
    </div>
    <div v-else-if="security" class="rounded-lg border border-slate-700/60 bg-surface px-5 py-4 text-xs space-y-2">
      <p class="text-slate-300">{{ $t('状态：') }} <span :class="security?.unlocked ? 'text-emerald-400' : 'text-amber-400'">
          {{ security.unlocked ? t('保险库已解锁') : security.needsUnlock ? t('等待主密码解锁') : t('保险库不可用') }}
        </span>
      </p>
      <p class="text-slate-500">{{ $t('主密码：') }}{{ security?.masterPasswordEnabled ? t('已启用') : t('未启用') }}{{ $t('· 钥匙串：') }}{{ security?.keyringAvailable ? t('可用') : t('不可用（已回退本地密钥文件）') }}
      </p>
    </div>

    <div v-if="security && !loading && !security.masterPasswordEnabled" class="neo">
      <div class="px-5 py-4 border-b border-slate-800/60">
        <p class="text-sm font-medium text-slate-200">{{ $t('启用启动主密码') }}</p>
        <p class="text-xs text-slate-500 mt-1">{{ $t('开启后每次启动需输入密码才能访问服务器与凭证。') }}</p>
      </div>
      <div class="px-5 py-4 space-y-3">
        <Field :label="$t('新主密码')" html-for="settings-new-master">
          <Input id="settings-new-master" v-model="secForm.password" type="password" :placeholder="$t('新主密码')" autocomplete="new-password" />
        </Field>
        <Field :label="$t('确认主密码')" html-for="settings-confirm-master">
          <Input id="settings-confirm-master" v-model="secForm.confirm" type="password" :placeholder="$t('确认主密码')" autocomplete="new-password" />
        </Field>
        <div class="flex items-center justify-between gap-3">
          <p v-if="secError" class="text-xs text-rose-400" role="alert">{{ secError }}</p>
          <p v-else-if="secMsg" class="text-xs text-emerald-400" role="status">{{ secMsg }}</p>
          <Button variant="primary" size="sm" class="ml-auto" :loading="secBusy" @click="enableMaster">{{ secBusy ? t('处理中…') : t('启用') }}</Button>
        </div>
      </div>
    </div>

    <template v-else-if="security && !loading">
      <div class="neo">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">{{ $t('更换主密码') }}</p>
        </div>
        <div class="px-5 py-4 space-y-3">
          <Field :label="$t('当前主密码')" html-for="settings-old-master">
            <Input id="settings-old-master" v-model="secForm.oldPassword" type="password" :placeholder="$t('当前主密码')" autocomplete="current-password" />
          </Field>
          <Field :label="$t('新主密码')" html-for="settings-change-master">
            <Input id="settings-change-master" v-model="secForm.newPassword" type="password" :placeholder="$t('新主密码')" autocomplete="new-password" />
          </Field>
          <Field :label="$t('确认新主密码')" html-for="settings-change-confirm">
            <Input id="settings-change-confirm" v-model="secForm.newConfirm" type="password" :placeholder="$t('确认新主密码')" autocomplete="new-password" />
          </Field>
          <div class="flex justify-end">
            <Button variant="primary" size="sm" :loading="secBusy" @click="changeMaster">{{ $t('更换') }}</Button>
          </div>
        </div>
      </div>

      <div class="neo">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">{{ $t('关闭主密码') }}</p>
          <p class="text-xs text-slate-500 mt-1">{{ $t('关闭后主密钥改存系统钥匙串（失败时回退本地密钥文件）。') }}</p>
        </div>
        <div class="px-5 py-4 space-y-3">
          <Field :label="$t('当前主密码')" html-for="settings-disable-master">
            <Input id="settings-disable-master" v-model="secForm.oldPassword" type="password" :placeholder="$t('当前主密码')" autocomplete="current-password" />
          </Field>
          <div class="flex items-center justify-between gap-3">
            <p v-if="secError" class="text-xs text-rose-400" role="alert">{{ secError }}</p>
            <p v-else-if="secMsg" class="text-xs text-emerald-400" role="status">{{ secMsg }}</p>
            <Button variant="danger" size="sm" class="ml-auto" :loading="secBusy" @click="disableMaster">{{ $t('关闭主密码') }}</Button>
          </div>
        </div>
      </div>
    </template>
    <HostTrustSection />
  </section>
</template>
