<script setup lang="ts">
import {Button, Field, Input} from '@/components/ui'
import appLogo from '@/assets/images/ding-logo.png'

defineProps<{unlocking: boolean; error: string}>()
const password = defineModel<string>({required: true})
const emit = defineEmits<{submit: []}>()
</script>

<template>
  <div class="absolute inset-0 z-50 grid place-items-center overflow-auto p-8">
    <div class="w-full max-w-[880px] grid grid-cols-1 md:grid-cols-[1.05fr_1fr] gap-10 items-center">
      <section class="hidden md:flex flex-col gap-5">
        <div class="flex items-center gap-3">
          <div class="brand-mark">
            <img :src="appLogo" alt="" class="brand-logo" />
          </div>
          <div>
            <div class="brand-name">ding<span>-ssh</span></div>
            <div class="text-[10.5px] tracking-[0.16em] uppercase text-mist mt-1">Grid Desk</div>
          </div>
        </div>
        <h1 class="text-[30px] font-semibold tracking-[-0.03em] leading-[1.15] text-[var(--foreground)]">解锁工作台</h1>
        <p class="text-[13px] leading-relaxed text-mist max-w-[380px]">
          主密码已启用。输入后解密服务器节点、凭证与隧道配置，进入本机会话。
        </p>
        <div class="panel p-4 flex flex-col gap-3 max-w-[400px]">
          <div class="flex items-baseline justify-between gap-4">
            <span class="text-[10.5px] uppercase tracking-[0.1em] text-mist">加密算法</span>
            <span class="font-mono text-[12px] text-[var(--foreground)]">AES-256-GCM</span>
          </div>
          <div class="flex items-baseline justify-between gap-4">
            <span class="text-[10.5px] uppercase tracking-[0.1em] text-mist">密钥派生</span>
            <span class="font-mono text-[12px] text-[var(--foreground)]">Argon2id</span>
          </div>
          <div class="flex items-baseline justify-between gap-4">
            <span class="text-[10.5px] uppercase tracking-[0.1em] text-mist">密钥存储</span>
            <span class="font-mono text-[12px] text-[var(--foreground)]">OS Keyring</span>
          </div>
        </div>
      </section>

      <form class="panel p-6 flex flex-col gap-4" @submit.prevent="emit('submit')">
        <div class="md:hidden flex items-center gap-3">
          <div class="brand-mark">
            <img :src="appLogo" alt="" class="brand-logo" />
          </div>
          <div class="brand-name">ding<span>-ssh</span></div>
        </div>
        <div>
          <h2 class="text-[17px] font-semibold text-[var(--foreground)]">输入主密码</h2>
          <p class="text-[12px] text-mist mt-1">解密本地保险库后进入工作台。</p>
        </div>
        <Field label="主密码" html-for="masterPwd">
          <Input
            id="masterPwd"
            v-model="password"
            type="password"
            placeholder="输入主密码"
            autocomplete="current-password"
            autofocus
          />
        </Field>
        <p class="text-[12px] text-danger min-h-4" role="alert">{{ error }}</p>
        <Button variant="primary" type="submit" class="w-full" style="height:36px" :loading="unlocking" :disabled="unlocking || !password">
          {{ unlocking ? '解密中…' : '解锁并进入' }}
        </Button>
      </form>
    </div>
  </div>
</template>
