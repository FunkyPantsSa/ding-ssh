<script lang="ts" setup>
import {computed, onMounted, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import type {Credential, ServerNode} from '../types'

/**
 * editing：编辑对象（null = 新建）。
 * defaultGroup：新建时的默认分组（来自服务器管理左栏当前选中的分组）。
 */
const props = defineProps<{editing: ServerNode | null; defaultGroup?: string}>()
const show = defineModel<boolean>({required: true})

const servers = useServersStore()
const credentials = useCredentialsStore()
const sessions = useSessionsStore()
const ui = useUIStore()
const saving = ref(false)
const testing = ref(false)
const errorMsg = ref('')
const testMsg = ref('')
const testOk = ref(false)
const keySource = ref<'file' | 'content'>('file')
const selectedCred = ref('')
/** 当前选中的凭证 id：用于「私钥内容不展示、保存时按 id 取回」的间接引用。 */
const credentialsUsed = ref('')

const groups = computed(() => {
  const set = new Set(servers.servers.map((s) => s.group.trim()).filter(Boolean))
  // 左栏选中的分组可能还没有任何服务器（手动建的空分组），也要出现在可选项里。
  const current = (props.defaultGroup ?? '').trim()
  if (current) set.add(current)
  return [...set].sort((a, b) => a.localeCompare(b))
})

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
    testMsg.value = ''
    testOk.value = false
    keySource.value = editing?.keyContent ? 'content' : 'file'
    selectedCred.value = ''
    credentialsUsed.value = ''
    // 新建时默认沿用调用方（服务器管理左栏）当前选中的分组；
    // 'all' 表示「全部服务器」，不是具体分组，回落为未分组。
    const preset = editing ? '' : (props.defaultGroup ?? '').trim()
    const presetGroup = preset === 'all' ? '' : preset
    Object.assign(form, {
      id: editing?.id ?? '',
      name: editing?.name ?? '',
      group: editing?.group ?? presetGroup,
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
    syncGroupFromForm(editing?.group ?? presetGroup)
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

/**
 * 解析私钥内容：优先用表单里用户自己粘贴的内容（编辑既有服务器时会回填），
 * 否则回退到「所选用保存凭证」里的私钥明文。
 *
 * 这样表单 / DOM 上永远不会出现凭证的私钥，而保存结果与直接粘贴凭证私钥完全一致。
 */
function resolveKeyContent(): string {
  const typed = (form.keyContent ?? '').trim()
  if (typed) return typed
  if (!credentialsUsed.value) return ''
  const c = credentials.list.find((x) => x.id === credentialsUsed.value)
  return (c?.keyContent ?? '').trim()
}

/**
 * 校验并构造待保存 / 待测试的服务器节点。
 *
 * - requireName：保存时需要名称，测试连接只关心登录凭据（主机 / 用户名 / 密码 / 私钥）。
 * - 私钥内容在此解析：既支持表单里粘贴的内容，也支持「所选凭证」里的私钥明文
 *   （resolveKeyContent 负责按 id 取回），同时不把明文写回 form，避免出现在 DOM 上。
 * - 校验失败时写入 errorMsg 并返回 null。
 */
function buildPayload(requireName: boolean): ServerNode | null {
  if (requireName && !form.name.trim()) {
    errorMsg.value = '请填写名称'
    return null
  }
  if (!form.host.trim()) {
    errorMsg.value = '请填写主机'
    return null
  }
  if (!form.user.trim()) {
    errorMsg.value = '请填写用户名'
    return null
  }
  const node: ServerNode = {...form}
  if (form.authType === 'password' && !form.password) {
    errorMsg.value = '请填写密码'
    return null
  }
  if (form.authType === 'privateKey') {
    if (keySource.value === 'file') {
      if (!(form.keyPath ?? '').trim()) {
        errorMsg.value = '请选择私钥文件'
        return null
      }
      // 文件来源：清掉可能残留的粘贴内容，优先用文件。
      node.keyContent = ''
    } else {
      const content = resolveKeyContent()
      if (!content) {
        errorMsg.value = usingStoredKeyContent.value
          ? '所选凭证的私钥内容为空，请重新选择或粘贴私钥'
          : '请粘贴私钥内容'
        return null
      }
      node.keyContent = content
      node.keyPath = ''
    }
  }
  return node
}

async function save() {
  applyGroupToForm()
  const payload = buildPayload(true)
  if (!payload) return
  saving.value = true
  errorMsg.value = ''
  try {
    await servers.save({...payload, group: payload.group.trim()})
    show.value = false
  } catch (e) {
    errorMsg.value = String(e)
  } finally {
    saving.value = false
  }
}

/** 保存并连接：保存成功后回到终端页，并新开一个标签连接刚保存的服务器。 */
async function saveAndConnect() {
  applyGroupToForm()
  const payload = buildPayload(true)
  if (!payload) return
  saving.value = true
  errorMsg.value = ''
  try {
    const saved = await servers.save({...payload, group: payload.group.trim()})
    show.value = false
    ui.showWorkspace()
    sessions.openTab(saved)
  } catch (e) {
    errorMsg.value = String(e)
  } finally {
    saving.value = false
  }
}

/** 测试连接：用当前表单的凭据实际完成一次 SSH 登录，不写入服务器列表。 */
async function testConnection() {
  const payload = buildPayload(false)
  if (!payload) return
  testing.value = true
  errorMsg.value = ''
  testMsg.value = ''
  testOk.value = false
  try {
    const res = await sshService.testServerLogin(payload)
    if (res.reachable) {
      testOk.value = true
      testMsg.value = `登录成功，耗时 ${res.latencyMs}ms`
    } else {
      testOk.value = false
      testMsg.value = `登录失败：${res.error || '未知错误'}`
    }
  } catch (e) {
    testOk.value = false
    testMsg.value = `登录失败：${String(e)}`
  } finally {
    testing.value = false
  }
}

/**
 * 从已保存凭证填充表单。
 *
 * 关键约束（需求：选择凭证时不展示私钥）：凭证里可能保存着私钥内容，这里只复制
 * 「用户名 / 认证方式 / 私钥文件路径 / 口令」，**绝不把凭证的私钥明文写进 form.keyContent**，
 * 因此界面上不会出现任何私钥文本。
 *   - 凭证带私钥文件路径 → 表单沿用该路径（keyContent 保持为空）；
 *   - 凭证只有私钥内容 → 表单只看得到「已使用保存的私钥」提示，明文留在凭证明文库里，
 *     由 credentialsUsed 标记在保存时从所选凭证取回。
 */
function applyCredential(id: string) {
  const c = credentials.list.find((x) => x.id === id)
  if (!c) return
  credentialsUsed.value = id
  form.user = c.user
  form.keyPath = ''
  form.keyContent = '' // 私钥明文永不落到表单 / DOM 上
  if (c.authType === 'privateKey') {
    form.authType = 'privateKey'
    form.password = c.password || ''
    form.keyPath = c.keyPath || ''
    // 凭证只存了私钥内容（没有文件路径）时切到「粘贴内容」分支：
    // 界面显示的是「已使用保存的私钥」提示，明文不展示，保存时按凭证 id 取回。
    keySource.value = c.keyPath ? 'file' : 'content'
  } else {
    form.authType = 'password'
    form.password = c.password || ''
    keySource.value = 'file'
  }
}

/** 清除凭证引用（用户改用手填内容时调用）。 */
function clearCredentialRef() {
  credentialsUsed.value = ''
}

/** 凭证下拉里的说明文案：明确标注认证方式（私钥类凭证不会展示私钥内容）。 */
function credentialLabel(c: Credential): string {
  const kind = c.authType === 'privateKey' ? '私钥' : '密码'
  return `${c.name}（${c.user} · ${kind}）`
}

/** 当前是否正在使用「只有私钥内容」的保存凭证（此时表单不展示私钥明文）。 */
const usingStoredKeyContent = computed(() => {
  if (!credentialsUsed.value || form.authType !== 'privateKey') return false
  const c = credentials.list.find((x) => x.id === credentialsUsed.value)
  return !!c && !c.keyPath && !form.keyPath
})

/** 私钥来源切换：手动选择来源即视为脱离凭证（避免两个来源互相覆盖）。 */
function setKeySource(src: 'file' | 'content') {
  keySource.value = src
  if (src === 'content' && usingStoredKeyContent.value) {
    // 从「用保存的私钥」切到「手动粘贴」：清掉引用，要求用户填入内容
    credentialsUsed.value = ''
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
              <span class="field-label">名称 *</span>
              <input
                v-model="form.name"
                class="input"
                placeholder="例如：生产服务器"
              />
            </label>
            <label class="block">
              <span class="field-label">端口</span>
              <input
                v-model.number="form.port"
                type="number"
                class="input"
              />
            </label>
          </div>

          <div class="block">
            <div class="flex items-center justify-between gap-2">
              <span class="field-label">分组（可选）</span>
              <div class="flex rounded-md border border-[var(--field-border)] overflow-hidden text-[12px]">
                <button
                  type="button"
                  class="px-2 py-0.5 transition-colors"
                  :class="groupMode === 'select' ? 'seg-opt-active' : 'seg-opt-idle'"
                  @click="groupMode = 'select'"
                >
                  选择已有
                </button>
                <button
                  type="button"
                  class="px-2 py-0.5 transition-colors border-l border-[var(--field-border)]"
                  :class="groupMode === 'create' ? 'seg-opt-active' : 'seg-opt-idle'"
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
              <span class="field-label">主机 / IP *</span>
              <input
                v-model="form.host"
                class="input"
                placeholder="example.com"
              />
            </label>
            <label class="block">
              <span class="field-label">用户名 *</span>
              <input
                v-model="form.user"
                class="input"
                placeholder="root"
              />
            </label>
          </div>

          <div>
            <span class="field-label">认证方式</span>
            <div class="seg">
              <button :class="form.authType === 'password' ? 'active' : ''" @click="form.authType = 'password'">密码</button>
              <button :class="form.authType === 'privateKey' ? 'active' : ''" @click="form.authType = 'privateKey'">私钥</button>
            </div>
          </div>

          <div v-if="form.authType === 'password'" class="space-y-3">
            <label class="block">
              <span class="field-label">密码 *</span>
              <input
                v-model="form.password"
                type="password"
                class="input"
                placeholder="登录密码"
              />
            </label>
            <label v-if="credentials.list.length" class="block">
              <span class="field-label">使用已保存凭证（可选）</span>
              <select
                v-model="selectedCred"
                class="select"
                @change="applyCredential(selectedCred)"
              >
                <option value="">— 不使用 —</option>
                <option v-for="c in credentials.list" :key="c.id" :value="c.id">
                  {{ credentialLabel(c) }}
                </option>
              </select>
              <p class="mt-1 text-[12px] text-slate-500">
                私钥类凭证同样可选：会带出用户名与认证方式，私钥内容不会显示在界面上。
              </p>
            </label>
          </div>

          <div v-else class="space-y-3">
            <div>
              <span class="field-label">私钥来源</span>
              <div class="mt-1 flex gap-2">
                <button
                  class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 transition-colors"
                  :class="keySource === 'file' ? 'field-surface-active' : 'field-surface'"
                  @click="setKeySource('file')"
                >
                  密钥文件
                </button>
                <button
                  class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 transition-colors"
                  :class="keySource === 'content' ? 'field-surface-active' : 'field-surface'"
                  @click="setKeySource('content')"
                >
                  粘贴内容
                </button>
              </div>
            </div>

            <div v-if="keySource === 'file'">
              <span class="field-label">私钥文件 *</span>
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
              <span class="field-label">私钥内容 *</span>
              <div
                v-if="usingStoredKeyContent"
                class="mt-1 rounded-md border border-[var(--field-border)] bg-[var(--hover)] px-3 py-3 text-[12px] leading-relaxed text-slate-400"
              >
                <div class="flex items-center gap-2 text-[var(--mist-100)]">
                  <Icon name="lock" :size="13" />
                  已使用保存的私钥
                </div>
                <p class="mt-1">
                  私钥内容保存在凭据库中，出于安全考虑不在界面上回显；保存后会自动写入该服务器。
                  需要换成别的私钥时，切换上方「密钥文件」，或重新选择 / 粘贴内容。
                </p>
              </div>
              <textarea
                v-else
                v-model="form.keyContent"
                rows="7"
                spellcheck="false"
                autocomplete="off"
                class="mt-1 w-full textarea text-xs leading-relaxed resize-y"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
                @input="clearCredentialRef"
              ></textarea>
            </div>

            <label v-if="credentials.list.length" class="block">
              <span class="field-label">使用已保存凭证（可选）</span>
              <select
                v-model="selectedCred"
                class="select"
                @change="applyCredential(selectedCred)"
              >
                <option value="">— 不使用 —</option>
                <option v-for="c in credentials.list" :key="c.id" :value="c.id">
                  {{ credentialLabel(c) }}
                </option>
              </select>
            </label>

            <label class="block">
              <span class="field-label">私钥口令（可选）</span>
              <input
                v-model="form.password"
                type="password"
                class="input"
                placeholder="加密私钥的 passphrase"
              />
            </label>
          </div>

          <p v-if="errorMsg" class="text-xs text-rose-400 break-all">{{ errorMsg }}</p>
          <p v-else-if="testMsg" class="text-xs break-all" :class="testOk ? 'text-[var(--ok-500)]' : 'text-rose-400'">
            <Icon :name="testOk ? 'check' : 'alert'" :size="13" />
            {{ testMsg }}
          </p>
        </div>

        <div class="flex justify-end gap-2 px-6 py-5">
          <button class="btn btn-ghost" :disabled="saving || testing" @click="close">取消</button>
          <button class="btn btn-ghost" :disabled="testing || saving" @click="testConnection">
            <Icon name="activity" :size="14" :extra-class="testing ? 'animate-spin' : ''" />
            {{ testing ? '测试中…' : '测试连接' }}
          </button>
          <button class="btn btn-ghost" :disabled="saving || testing" @click="save">
            {{ saving ? '保存中…' : '保存' }}
          </button>
          <button class="btn btn-primary" :disabled="saving || testing" @click="saveAndConnect">
            <Icon name="check" :size="14" />
            保存并连接
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
