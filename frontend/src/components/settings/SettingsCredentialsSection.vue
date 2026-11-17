<script lang="ts" setup>
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {onMounted} from 'vue'
import Icon from '../Icon.vue'
import CredentialDialog from '../CredentialDialog.vue'
import {Button, Dialog, EmptyState, Textarea} from '../ui'
import {useSettingsCredentials} from '../../composables/useSettingsCredentials'

const {
  credentials,
  showDialog,
  confirmId,
  editing,
  revealId,
  revealKey,
  copiedId,
  openNew,
  openEdit,
  toggleReveal,
  copy,
  remove,
} = useSettingsCredentials()

onMounted(() => {
  if (!credentials.loaded) void credentials.load()
})
</script>

<template>
  <section class="max-w-2xl fade-rise" data-section-content="credentials" aria-labelledby="settings-credentials-title">
    <div class="mb-6 flex items-start justify-between gap-4">
      <div>
        <h3 id="settings-credentials-title" class="text-[18px] font-semibold text-white tracking-tight">{{ $t('保存的凭证') }}</h3>
        <p class="text-[13px] text-mist mt-1.5">{{ $t('保存常用用户名密码或私钥，新建服务器时可直接选择自动填充。') }}</p>
      </div>
      <Button variant="primary" size="sm" class="shrink-0" @click="openNew">
        <Icon name="plus" :size="14" />{{ $t('新增凭证') }}</Button>
    </div>

    <div class="neo">
      <div class="px-5 py-4 space-y-2">
        <EmptyState v-if="!credentials.list.length" :title="$t('暂无凭证')" :description="$t('点击右上角「新增凭证」添加，新建服务器时可直接选择。')" />
        <template v-else>
        <div
          v-for="c in credentials.list"
          :key="c.id"
          class="flex items-center gap-3 px-3 py-2 rounded-md bg-surface-raised border border-slate-700/40"
        >
          <div class="min-w-0 flex-1">
            <p class="text-[13px] text-slate-200 truncate">{{ c.name }}</p>
            <p class="text-[12px] text-slate-500 truncate">
              {{ c.user }} · {{ c.authType === 'privateKey' ? t('RSA 私钥') : t('密码') }}
              <span class="font-mono text-slate-400">
                {{ c.authType === 'privateKey' ? '••••••••' : revealId === c.id ? c.password || '' : '••••••••' }}
              </span>
            </p>
          </div>
          <div v-if="confirmId !== c.id" class="flex items-center gap-1 shrink-0">
            <Button variant="ghost" size="icon" :title="revealId === c.id ? t('隐藏') : t('查看密码')" :aria-label="revealId === c.id ? t('隐藏密码') : t('查看密码')" @click="toggleReveal(c)">
              <Icon name="eye" :size="14" />
            </Button>
            <Button variant="ghost" size="icon" :title="c.authType === 'privateKey' ? t('复制私钥') : t('复制密码')" :aria-label="c.authType === 'privateKey' ? t('复制私钥') : t('复制密码')" @click="copy(c)">
              <Icon :name="copiedId === c.id ? 'check' : 'copy'" :size="14" :class="copiedId === c.id ? 'text-[var(--success)]' : ''" />
            </Button>
            <Button variant="ghost" size="icon" :title="$t('编辑')" :aria-label="$t('编辑凭证')" @click="openEdit(c)">
              <Icon name="pencil" :size="14" />
            </Button>
            <Button variant="ghost" size="icon" :title="$t('删除')" :aria-label="$t('删除凭证')" @click="confirmId = c.id">
              <Icon name="trash" :size="14" />
            </Button>
          </div>
          <div v-else class="flex items-center gap-1 shrink-0">
            <Button variant="danger" size="sm" @click="remove(c.id)">{{ $t('确认') }}</Button>
            <Button variant="ghost" size="sm" @click="confirmId = ''">{{ $t('取消') }}</Button>
          </div>
        </div>
        </template>
      </div>
    </div>

    <CredentialDialog v-model="showDialog" :editing="editing" />

    <Dialog :model-value="!!revealKey" :title="$t('RSA 私钥内容')" :width="560" @update:model-value="(v) => { if (!v) revealKey = null }">
      <p class="text-[12px] text-mist mb-3">「{{ revealKey?.name }} {{ $t('」保存的私钥明文，仅保存在本机。') }}</p>
      <Textarea :model-value="revealKey?.keyContent || ''" readonly :rows="10" class="font-mono" :aria-label="$t('RSA 私钥内容')" />
      <template #footer>
        <Button variant="ghost" @click="revealKey = null">{{ $t('关闭') }}</Button>
        <Button variant="primary" @click="revealKey && copy(revealKey); revealKey = null">{{ $t('复制私钥') }}</Button>
      </template>
    </Dialog>
  </section>
</template>
