<script lang="ts" setup>
import {errorText} from '../i18n'

import {t as $t} from '../i18n'

import {t} from '../i18n'

import {reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import type {Credential} from '../types'
import {Button, Dialog, Field, Input, Textarea} from './ui'

const props = defineProps<{editing: Credential | null}>()
const show = defineModel<boolean>({required: true})

const credentials = useCredentialsStore()
const saving = ref(false)
const errorMsg = ref('')
const keySource = ref<'file' | 'content'>('file')

const form = reactive({
  id: '',
  name: '',
  user: '',
  password: '',
  authType: 'password',
  keyPath: '',
  keyContent: '',
})

watch(
  () => [show.value, props.editing] as const,
  ([open, editing]) => {
    if (!open) return
    errorMsg.value = ''
    keySource.value = editing?.keyContent ? 'content' : 'file'
    Object.assign(form, {
      id: editing?.id ?? '',
      name: editing?.name ?? '',
      user: editing?.user ?? '',
      password: editing?.password ?? '',
      authType: editing?.authType ?? 'password',
      keyPath: editing?.keyPath ?? '',
      keyContent: editing?.keyContent ?? '',
    })
  },
)

async function pickKeyFile() {
  try {
    const path = await sshService.selectKeyFile()
    if (path) form.keyPath = path
  } catch (e) {
    errorMsg.value = errorText(e)
  }
}

async function save() {
  if (!form.name.trim() || !form.user.trim()) {
    errorMsg.value = t("请填写凭证名称和用户名")
    return
  }
  if (form.authType === 'password' && !form.password) {
    errorMsg.value = t("请填写密码")
    return
  }
  if (form.authType === 'privateKey') {
    if (keySource.value === 'file') {
      if (!form.keyPath.trim()) {
        errorMsg.value = t("请选择私钥文件")
        return
      }
      form.keyContent = ''
    } else {
      if (!form.keyContent.trim()) {
        errorMsg.value = t("请粘贴私钥内容")
        return
      }
      form.keyPath = ''
    }
  }
  saving.value = true
  errorMsg.value = ''
  try {
    await credentials.save({...form})
    show.value = false
  } catch (e) {
    errorMsg.value = errorText(e)
  } finally {
    saving.value = false
  }
}

function close() {
  if (!saving.value) show.value = false
}
</script>

<template>
  <Dialog v-model="show" :title="editing ? t('编辑凭证') : t('新建凭证')" :width="520" :close-disabled="saving">
    <div class="flex flex-col gap-4">
      <p class="text-[12px] text-mist">{{ $t('保存常用用户名密码或私钥，新建服务器时可直接选择自动填充。') }}</p>

      <div class="grid grid-cols-2 gap-3">
        <Field :label="$t('名称 *')" html-for="credential-name">
          <Input id="credential-name" v-model="form.name" :placeholder="$t('例如：生产 root')" autocomplete="off" />
        </Field>
        <Field :label="$t('用户名 *')" html-for="credential-user">
          <Input id="credential-user" v-model="form.user" placeholder="root" autocomplete="off" />
        </Field>
      </div>

      <div>
        <span class="field-label">{{ $t('认证方式') }}</span>
        <div class="seg mt-1">
          <button type="button" :class="form.authType === 'password' ? 'active' : ''" :aria-pressed="form.authType === 'password'" @click="form.authType = 'password'">{{ $t('密码') }}</button>
          <button type="button" :class="form.authType === 'privateKey' ? 'active' : ''" :aria-pressed="form.authType === 'privateKey'" @click="form.authType = 'privateKey'">{{ $t('私钥') }}</button>
        </div>
      </div>

      <Field v-if="form.authType === 'password'" :label="$t('密码 *')" html-for="credential-password">
        <Input id="credential-password" v-model="form.password" type="password" :placeholder="$t('登录密码')" autocomplete="current-password" />
      </Field>

      <template v-else>
        <div>
          <span class="field-label">{{ $t('私钥来源') }}</span>
          <div class="mt-1 flex gap-2">
            <Button class="flex-1" :variant="keySource === 'file' ? 'secondary' : 'ghost'" :aria-pressed="keySource === 'file'" @click="keySource = 'file'">{{ $t('密钥文件') }}</Button>
            <Button class="flex-1" :variant="keySource === 'content' ? 'secondary' : 'ghost'" :aria-pressed="keySource === 'content'" @click="keySource = 'content'">{{ $t('粘贴内容') }}</Button>
          </div>
        </div>

        <Field v-if="keySource === 'file'" :label="$t('私钥文件 *')" html-for="credential-key-path">
          <div class="flex gap-2">
            <Input id="credential-key-path" v-model="form.keyPath" readonly class="flex-1" placeholder="~/.ssh/id_rsa" />
            <Button variant="secondary" size="sm" @click="pickKeyFile">{{ $t('选择…') }}</Button>
          </div>
        </Field>

        <div v-else>
          <span class="field-label">{{ $t('私钥内容 *') }}</span>
          <Textarea
            v-model="form.keyContent"
            rows="7"
            spellcheck="false"
            class="mt-1 font-mono"
            :aria-label="$t('私钥内容')"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
          />
        </div>

        <Field :label="$t('私钥口令（可选）')" html-for="credential-key-passphrase">
          <Input id="credential-key-passphrase" v-model="form.password" type="password" :placeholder="$t('加密私钥的 passphrase')" autocomplete="current-password" />
        </Field>
      </template>

      <p v-if="errorMsg" class="text-xs text-rose-400 break-all" role="alert">{{ errorMsg }}</p>
    </div>

    <template #footer>
      <Button variant="ghost" :disabled="saving" @click="close">{{ $t('取消') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="saving" @click="save">
        {{ saving ? t('保存中…') : editing ? t('保存修改') : t('创建凭证') }}
      </Button>
    </template>
  </Dialog>
</template>
