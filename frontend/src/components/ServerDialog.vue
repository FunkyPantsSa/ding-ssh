<script lang="ts" setup>
import {computed, onMounted, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import {useServersStore} from '../stores/servers'
import type {ServerNode} from '../types'

const props = defineProps<{editing: ServerNode | null}>()
const show = defineModel<boolean>({required: true})

const servers = useServersStore()
const credentials = useCredentialsStore()
const saving = ref(false)
const errorMsg = ref('')
const keySource = ref<'file' | 'content'>('file')
const selectedCred = ref('')

const groups = computed(() =>
  [...new Set(servers.servers.map((s) => s.group.trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b)),
)

/** select = 从已有分组选择；create = 手动输入新建分组名 */
const groupMode = ref<'select' | 'create'>('select')
const selectedGroup = ref('')
const newGroupName = ref('')

const form = reactive<ServerNode>({
  id: '',
  name: '',
  group: '',
  host: '',
  port: 22,
  user: 'root',
  authType: 'password',
  password: '',
  keyPath: '',
  keyContent: '',
  bgImage: '',
  blurAmount: 12,
  envVars: {},
})

function syncGroupFromForm(group: string) {
  const g = (group || '').trim()
  if (!g) {
    groupMode.value = 'select'
    selectedGroup.value = ''
    newGroupName.value = ''
    return
  }
  if (groups.value.includes(g)) {
    groupMode.value = 'select'
    selectedGroup.value = g
    newGroupName.value = ''
  } else {
    groupMode.value = 'create'
    selectedGroup.value = ''
    newGroupName.value = g
  }
}

function applyGroupToForm() {
  if (groupMode.value === 'create') {
    form.group = newGroupName.value.trim()
  } else {
    form.group = selectedGroup.value.trim()
  }
}

watch(groupMode, (mode) => {
  if (mode === 'select') {
    newGroupName.value = ''
    form.group = selectedGroup.value.trim()
  } else {
    form.group = newGroupName.value.trim()
  }
})

watch(selectedGroup, () => {
  if (groupMode.value === 'select') form.group = selectedGroup.value.trim()
})

watch(newGroupName, () => {
  if (groupMode.value === 'create') form.group = newGroupName.value.trim()
})

watch(
  () => [show.value, props.editing] as const,
  ([open, editing]) => {
    if (!open) return
    errorMsg.value = ''
    keySource.value = editing?.keyContent ? 'content' : 'file'
    selectedCred.value = ''
    Object.assign(form, {
      id: editing?.id ?? '',
      name: editing?.name ?? '',
      group: editing?.group ?? '',
      host: editing?.host ?? '',
      port: editing?.port ?? 22,
      user: editing?.user ?? 'root',
      authType: editing?.authType ?? 'password',
      password: editing?.password ?? '',
      keyPath: editing?.keyPath ?? '',
      keyContent: editing?.keyContent ?? '',
      bgImage: editing?.bgImage ?? '',
      blurAmount: editing?.blurAmount ?? 12,
      envVars: {},
    })
    syncGroupFromForm(editing?.group ?? '')
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
  if (!form.name.trim() || !form.host.trim() || !form.user.trim()) {
    errorMsg.value = '请填写名称、主机和用户名'
    return
  }
  if (form.authType === 'password' && !form.password) {
    errorMsg.value = '请填写密码'
    return
  }
  if (form.authType === 'privateKey') {
    if (keySource.value === 'file') {
      if (!(form.keyPath ?? '').trim()) {
        errorMsg.value = '请选择私钥文件'
        return
      }
      form.keyContent = ''
    } else {
      if (!(form.keyContent ?? '').trim()) {
        errorMsg.value = '请粘贴私钥内容'
        return
      }
      form.keyPath = ''
    }
  }
  saving.value = true
  errorMsg.value = ''
  try {
    applyGroupToForm()
    await servers.save({...form, group: form.group.trim()})
    show.value = false
  } catch (e) {
    errorMsg.value = String(e)
  } finally {
    saving.value = false
  }
}

function applyCredential(id: string) {
  const c = credentials.list.find((x) => x.id === id)
  if (c) {
    form.user = c.user
    if (c.authType === 'privateKey') {
      form.authType = 'privateKey'
      form.password = c.password || ''
      form.keyPath = c.keyPath || ''
      form.keyContent = c.keyContent || ''
      keySource.value = c.keyContent ? 'content' : 'file'
    } else {
      form.authType = 'password'
      form.password = c.password || ''
    }
  }
}

function close() {
  if (!saving.value) show.value = false
}

onMounted(() => {
  if (!credentials.loaded) void credentials.load()
})
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="modal-root" @click.self="close">
      <div class="modal neo wide" style="width:min(560px,92vw);max-height:85vh;display:flex;flex-direction:column">
        <div class="flex items-center justify-between px-6 pt-6 pb-4">
          <div>
            <h3>{{ editing ? '编辑服务器' : '新建服务器' }}</h3>
            <p class="mdesc !mb-0">配置主机、认证方式与分组。私钥内容本地加密存储。</p>
          </div>
          <button class="btn-icon" title="关闭" :disabled="saving" @click="close">
            <Icon name="close" :size="14" />
          </button>
        </div>

        <div class="flex-1 overflow-y-auto px-6 pb-2 flex flex-col gap-4">
          <div class="grid grid-cols-2 gap-3">
            <label class="block">
              <span class="text-slate-400">名称 *</span>
              <input
                v-model="form.name"
                class="input"
                placeholder="例如：生产服务器"
              />
            </label>
            <label class="block">
              <span class="text-slate-400">端口</span>
              <input
                v-model.number="form.port"
                type="number"
                class="input"
              />
            </label>
          </div>

          <div class="block">
            <div class="flex items-center justify-between gap-2">
              <span class="text-slate-400">分组（可选）</span>
              <div class="flex rounded-md border border-slate-700/60 overflow-hidden text-[12px]">
                <button
                  type="button"
                  class="px-2 py-0.5 transition-colors"
                  :class="groupMode === 'select' ? 'bg-sky-500/20 text-sky-300' : 'bg-slate-800/60 text-slate-500 hover:text-slate-300'"
                  @click="groupMode = 'select'"
                >
                  选择已有
                </button>
                <button
                  type="button"
                  class="px-2 py-0.5 transition-colors border-l border-slate-700/60"
                  :class="groupMode === 'create' ? 'bg-sky-500/20 text-sky-300' : 'bg-slate-800/60 text-slate-500 hover:text-slate-300'"
                  @click="groupMode = 'create'"
                >
                  新建分组
                </button>
              </div>
            </div>
            <select
              v-if="groupMode === 'select'"
              v-model="selectedGroup"
              class="select"
            >
              <option value="">未分组</option>
              <option v-for="g in groups" :key="g" :value="g">{{ g }}</option>
            </select>
            <input
              v-else
              v-model="newGroupName"
              type="text"
              autocomplete="off"
              autocorrect="off"
              autocapitalize="off"
              spellcheck="false"
              name="ding-ssh-new-group"
              class="input"
              placeholder="输入新分组名称"
            />
            <p class="mt-1 text-[12px] text-slate-500">
              {{ groupMode === 'select' ? '从已有分组中选择；无合适项可切换到「新建分组」。' : '输入新名称保存后即出现在分组列表中。' }}
            </p>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <label class="block">
              <span class="text-slate-400">主机 / IP *</span>
              <input
                v-model="form.host"
                class="input"
                placeholder="example.com"
              />
            </label>
            <label class="block">
              <span class="text-slate-400">用户名 *</span>
              <input
                v-model="form.user"
                class="input"
                placeholder="root"
              />
            </label>
          </div>

          <div>
            <span class="text-slate-400">认证方式</span>
            <div class="seg">
              <button :class="form.authType === 'password' ? 'active' : ''" @click="form.authType = 'password'">密码</button>
              <button :class="form.authType === 'privateKey' ? 'active' : ''" @click="form.authType = 'privateKey'">私钥</button>
            </div>
          </div>

          <div v-if="form.authType === 'password'" class="space-y-3">
            <label class="block">
              <span class="text-slate-400">密码 *</span>
              <input
                v-model="form.password"
                type="password"
                class="input"
                placeholder="登录密码"
              />
            </label>
            <label v-if="credentials.list.length" class="block">
              <span class="text-slate-400">使用已保存凭证（可选）</span>
              <select
                v-model="selectedCred"
                class="select"
                @change="applyCredential(selectedCred)"
              >
                <option value="">— 不使用 —</option>
                <option v-for="c in credentials.list" :key="c.id" :value="c.id">
                  {{ c.name }}（{{ c.user }}）
                </option>
              </select>
            </label>
          </div>

          <div v-else class="space-y-3">
            <div>
              <span class="text-slate-400">私钥来源</span>
              <div class="mt-1 flex gap-2">
                <button
                  class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 transition-colors"
                  :class="keySource === 'file' ? 'border-sky-500/70 bg-sky-500/10' : 'border-slate-700/60 bg-slate-800/60'"
                  @click="keySource = 'file'"
                >
                  密钥文件
                </button>
                <button
                  class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 transition-colors"
                  :class="keySource === 'content' ? 'border-sky-500/70 bg-sky-500/10' : 'border-slate-700/60 bg-slate-800/60'"
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
                  class="flex-1 min-w-0 px-3 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none"
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
                class="mt-1 w-full px-3 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs leading-relaxed outline-none focus:border-sky-500/60 resize-y"
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
          </div>

          <p v-if="errorMsg" class="text-xs text-rose-400 break-all">{{ errorMsg }}</p>
        </div>

        <div class="flex justify-end gap-2 px-6 py-5">
          <button class="btn btn-ghost" :disabled="saving" @click="close">取消</button>
          <button class="btn btn-primary" :disabled="saving" @click="save">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
