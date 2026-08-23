<script lang="ts" setup>
import {reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import type {Credential} from '../types'

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
    errorMsg.value = String(e)
  }
}

async function save() {
  if (!form.name.trim() || !form.user.trim()) {
    errorMsg.value = '请填写凭证名称和用户名'
    return
  }
  if (form.authType === 'password' && !form.password) {
    errorMsg.value = '请填写密码'
    return
  }
  if (form.authType === 'privateKey') {
    if (keySource.value === 'file') {
      if (!form.keyPath.trim()) {
        errorMsg.value = '请选择私钥文件'
        return
      }
      form.keyContent = ''
    } else {
      if (!form.keyContent.trim()) {
        errorMsg.value = '请粘贴私钥内容'
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
    errorMsg.value = String(e)
  } finally {
    saving.value = false
  }
}

function close() {
  if (!saving.value) show.value = false
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="modal-root" @click.self="close">
      <div class="modal neo wide" style="width:min(520px,92vw);max-height:85vh;display:flex;flex-direction:column">
        <div class="flex items-center justify-between px-6 pt-6 pb-4">
          <div>
            <h3>{{ editing ? '编辑凭证' : '新建凭证' }}</h3>
            <p class="mdesc !mb-0">保存常用用户名密码或私钥，新建服务器时可直接选择自动填充。</p>
          </div>
          <button class="btn-icon" title="关闭" :disabled="saving" @click="close">
            <Icon name="close" :size="14" />
          </button>
        </div>

        <div class="flex-1 overflow-y-auto px-6 pb-2 flex flex-col gap-4">
          <div class="grid grid-cols-2 gap-3">
            <label class="block">
              <span class="text-slate-400">名称 *</span>
              <input v-model="form.name" class="input" placeholder="例如：生产 root" />
            </label>
            <label class="block">
              <span class="text-slate-400">用户名 *</span>
              <input v-model="form.user" class="input" placeholder="root" />
            </label>
          </div>

          <div>
            <span class="text-slate-400">认证方式</span>
            <div class="seg">
              <button :class="form.authType === 'password' ? 'active' : ''" @click="form.authType = 'password'">密码</button>
              <button :class="form.authType === 'privateKey' ? 'active' : ''" @click="form.authType = 'privateKey'">私钥</button>
            </div>
          </div>

          <template v-if="form.authType === 'password'">
            <label class="block">
              <span class="text-slate-400">密码 *</span>
              <input v-model="form.password" type="password" class="input" placeholder="登录密码" />
            </label>
          </template>

          <template v-else>
            <div>
              <span class="text-slate-400">私钥来源</span>
              <div class="mt-1 flex gap-2">
                <button
                  class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 transition-colors"
                  :class="keySource === 'file' ? 'field-surface-active' : 'field-surface'"
                  @click="keySource = 'file'"
                >
                  密钥文件
                </button>
                <button
                  class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 transition-colors"
                  :class="keySource === 'content' ? 'field-surface-active' : 'field-surface'"
                  @click="keySource = 'content'"
                >
                  粘贴内容
                </button>
              </div>
            </div>

            <div v-if="keySource === 'file'">
              <span class="text-slate-400">私钥文件 *</span>
              <div class="mt-1 flex gap-2">
                <input
                  v-model="form.keyPath"
                  readonly
                  class="input input-sm flex-1 min-w-0"
                  placeholder="~/.ssh/id_rsa"
                />
                <button
                  class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 shrink-0"
                  @click="pickKeyFile"
                >
                  选择…
                </button>
              </div>
            </div>

            <div v-else>
              <span class="text-slate-400">私钥内容 *</span>
              <textarea
                v-model="form.keyContent"
                rows="7"
                spellcheck="false"
                class="mt-1 w-full textarea text-xs leading-relaxed resize-y"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
              ></textarea>
            </div>

            <label class="block">
              <span class="text-slate-400">私钥口令（可选）</span>
              <input
                v-model="form.password"
                type="password"
                class="input"
                placeholder="加密私钥的 passphrase"
              />
            </label>
          </template>

          <p v-if="errorMsg" class="text-xs text-rose-400 break-all">{{ errorMsg }}</p>
        </div>

        <div class="flex justify-end gap-2 px-6 py-5">
          <button class="btn btn-ghost" :disabled="saving" @click="close">取消</button>
          <button class="btn btn-primary" :disabled="saving" @click="save">
            {{ saving ? '保存中…' : editing ? '保存修改' : '创建凭证' }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
