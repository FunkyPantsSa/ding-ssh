<script lang="ts" setup>
import {errorText} from '../i18n'

import {t as $t} from '../i18n'

import {t} from '../i18n'

import {computed, onMounted, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import {useServersStore} from '../stores/servers'
import {useGroupsStore} from '../stores/groups'
import {normalizeGroupPath, validateGroupNameInput} from '../utils/groupTree'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import type {Credential, ServerNode} from '../types'
import SearchSelect from './ui/select/SearchSelect.vue'
import {Button, Dialog, Field, Input, Textarea} from './ui'

/**
 * editing：编辑对象（null = 新建）。
 * defaultGroup：新建时的默认分组（来自服务器管理左栏当前选中的分组）。
 */
const props = defineProps<{editing: ServerNode | null; defaultGroup?: string}>()
const show = defineModel<boolean>({required: true})

const servers = useServersStore()
const groupStore = useGroupsStore()
const credentials = useCredentialsStore()
const sessions = useSessionsStore()
const ui = useUIStore()
const saving = ref(false)
const testing = ref(false)
const errorMsg = ref('')
const testMsg = ref('')
const testOk = ref(false)
const agentIdentities = ref<Array<{keyType: string; fingerprint: string}>>([])
async function inspectAgent() {
 try { agentIdentities.value = await (window as any).go.main.App.GetAgentIdentities({...form}); errorMsg.value = '' }
 catch (e) { errorMsg.value = errorText(e) }
}
const keySource = ref<'file' | 'content'>('file')
const selectedCred = ref('')
/** 当前选中的凭证 id：用于「私钥内容不展示、保存时按 id 取回」的间接引用。 */
const credentialsUsed = ref('')

const groupOptions = computed(() => {
  const set = new Set<string>()
  for (const group of groupStore.list) {
    const path = normalizeGroupPath(group)
    if (path) set.add(path)
  }
  for (const server of servers.servers) {
    const path = normalizeGroupPath(server.group)
    if (path) set.add(path)
  }
  // 左栏选中的分组可能还没有任何服务器（手动建的空分组/隐式父级），也要出现在可选项里。
  // 'all' 是旧版「全部服务器」的哨兵值，不是真实分组，不能出现在下拉选项里。
  const current = normalizeGroupPath(props.defaultGroup ?? '')
  if (current && current !== 'all') set.add(current)
  return [...set].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN', {numeric: true}))
})

/** select = 从已有分组选择；create = 手动输入新建分组名 */
const groupMode = ref<'select' | 'create'>('select')
const selectedGroup = ref('')
const newGroupName = ref('')

/** 高级连接配置（跳板机 / 主机密钥别名 / known_hosts）默认折叠，已有配置时自动展开。 */
const advancedOpen = ref(false)

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

const configuredAdvanced = computed(() => !!(
  form.jumpServerMode ||
  form.jumpServerIds?.length ||
  form.hostKeyAlias?.trim() ||
  form.knownHostsSources?.length
))

function syncGroupFromForm(group: string) {
  const g = (group || '').trim()
  if (!g) {
    groupMode.value = 'select'
    selectedGroup.value = ''
    newGroupName.value = ''
    return
  }
  if (groupOptions.value.includes(g)) {
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
      identityFiles: [...(editing?.identityFiles ?? [])],
      identityAgent: editing?.identityAgent ?? '',
      identitiesOnly: editing?.identitiesOnly ?? false,
      jumpServerMode: editing?.jumpServerMode ?? false,
      jumpServerIds: [...(editing?.jumpServerIds ?? [])],
      hostKeyAlias: editing?.hostKeyAlias ?? '',
      knownHostsSources: [...(editing?.knownHostsSources ?? [])],
      importAlias: editing?.importAlias,
      importSource: editing?.importSource,
      password: editing?.password ?? '',
      keyPath: editing?.keyPath ?? '',
      keyContent: editing?.keyContent ?? '',
      bgImage: editing?.bgImage ?? '',
      blurAmount: editing?.blurAmount ?? 12,
      envVars: {},
    })
    syncGroupFromForm(editing?.group ?? presetGroup)
    advancedOpen.value = configuredAdvanced.value
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
    errorMsg.value = t("请填写名称")
    return null
  }
  if (!form.host.trim()) {
    errorMsg.value = t("请填写主机")
    return null
  }
  if (!form.user.trim()) {
    errorMsg.value = t("请填写用户名")
    return null
  }
  const groupPath = normalizeGroupPath(form.group)
  if (groupPath) {
    const groupError = validateGroupNameInput(groupPath, groupMode.value === 'create' ? 6 : 8)
    if (groupError) {
      errorMsg.value = groupError
      return null
    }
    form.group = groupPath
  }
  const node: ServerNode = {...form}
  if (form.authType === 'password' && !form.password) {
    errorMsg.value = t("请填写密码")
    return null
  }
  if (form.authType === 'privateKey') {
    if (keySource.value === 'file') {
      if (!(form.keyPath ?? '').trim()) {
        errorMsg.value = t("请选择私钥文件")
        return null
      }
      // 文件来源：清掉可能残留的粘贴内容，优先用文件。
      node.keyContent = ''
    } else {
      const content = resolveKeyContent()
      if (!content) {
        errorMsg.value = usingStoredKeyContent.value
          ? t("所选凭证的私钥内容为空，请重新选择或粘贴私钥")
          : t("请粘贴私钥内容")
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
    const saved = await servers.save({...payload, group: payload.group.trim()})
    sessions.syncServerNode(saved)
    show.value = false
  } catch (e) {
    errorMsg.value = errorText(e)
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
    sessions.syncServerNode(saved)
    show.value = false
    ui.showWorkspace()
    sessions.openTab(saved)
  } catch (e) {
    errorMsg.value = errorText(e)
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
      testMsg.value = t("登录成功，耗时 {0}ms", [res.latencyMs])
    } else {
      testOk.value = false
      testMsg.value = t("登录失败：{0}", [res.error || '未知错误'])
    }
  } catch (e) {
    testOk.value = false
    testMsg.value = t("登录失败：{0}", [errorText(e)])
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
  const kind = c.authType === 'privateKey' ? t("私钥") : t("密码")
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
  <Dialog v-model="show" :title="editing ? t('编辑服务器') : t('新建服务器')" :width="560" :close-disabled="saving || testing">
    <div class="flex flex-col gap-4">
      <p class="text-[12px] text-mist">{{ $t('配置主机、认证方式与分组。私钥内容本地加密存储。') }}</p>

      <div class="grid grid-cols-2 gap-3">
        <Field :label="$t('名称 *')" html-for="server-name">
          <Input id="server-name" v-model="form.name" :placeholder="$t('例如：生产服务器')" autocomplete="off" />
        </Field>
        <Field :label="$t('端口')" html-for="server-port">
          <Input
            id="server-port"
            :model-value="form.port"
            type="number"
            min="1"
            max="65535"
            @update:model-value="form.port = Number($event)"
          />
        </Field>
      </div>

      <div>
        <div class="flex items-center justify-between gap-2">
          <span class="field-label">{{ $t('分组（可选）') }}</span>
          <div class="flex gap-1">
            <Button
              type="button"
              size="sm"
              :variant="groupMode === 'select' ? 'secondary' : 'ghost'"
              :aria-pressed="groupMode === 'select'"
              @click="groupMode = 'select'"
            >{{ $t('选择已有') }}</Button>
            <Button
              type="button"
              size="sm"
              :variant="groupMode === 'create' ? 'secondary' : 'ghost'"
              :aria-pressed="groupMode === 'create'"
              @click="groupMode = 'create'"
            >{{ $t('新建分组') }}</Button>
          </div>
        </div>
        <SearchSelect
          v-if="groupMode === 'select'"
          v-model="selectedGroup"
          class="mt-1"
          :aria-label="$t('服务器分组')"
          :options="[{value: '', label: t('未分组')}, ...groupOptions.map(g => ({value: g, label: g}))]"
        />
        <Input
          v-else
          v-model="newGroupName"
          type="text"
          autocomplete="off"
          autocorrect="off"
          autocapitalize="off"
          spellcheck="false"
          name="ding-ssh-new-group"
          class="mt-1"
          :aria-label="$t('新分组名称')"
          :placeholder="$t('输入新分组名称')"
        />
        <p class="mt-1 text-[12px] text-slate-500">
          {{ groupMode === 'select' ? t('从已有分组中选择；无合适项可切换到「新建分组」。') : t('输入新名称保存后即出现在分组列表中。') }}
        </p>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <Field :label="$t('主机 / IP *')" html-for="server-host">
          <Input id="server-host" v-model="form.host" placeholder="example.com" autocomplete="off" />
        </Field>
        <Field :label="$t('用户名 *')" html-for="server-user">
          <Input id="server-user" v-model="form.user" placeholder="root" autocomplete="off" />
        </Field>
      </div>

      <div>
        <span class="field-label">{{ $t('认证方式') }}</span>
        <div class="seg mt-1">
          <button type="button" :class="form.authType === 'password' ? 'active' : ''" :aria-pressed="form.authType === 'password'" @click="form.authType = 'password'">{{ $t('密码') }}</button>
          <button type="button" :class="form.authType === 'privateKey' ? 'active' : ''" :aria-pressed="form.authType === 'privateKey'" @click="form.authType = 'privateKey'">{{ $t('私钥') }}</button>
          <button type="button" :class="form.authType === 'agent' ? 'active' : ''" @click="form.authType = 'agent'">SSH Agent</button>
          <button type="button" :class="form.authType === 'auto' ? 'active' : ''" @click="form.authType = 'auto'">{{ $t('自动选择') }}</button>
        </div>
      </div>

      <div v-if="form.authType === 'agent' || form.authType === 'auto'" class="space-y-3">
        <Field :label="$t('Agent 地址（留空使用系统默认）')"><Input v-model="form.identityAgent" placeholder="SSH_AUTH_SOCK / Windows OpenSSH Agent" /></Field>
        <Field :label="$t('身份文件（每行一个路径）')"><Textarea :model-value="(form.identityFiles ?? []).join('\n')" @update:model-value="value => form.identityFiles = String(value).split('\n').filter(Boolean)" /></Field>
        <Button size="sm" @click="inspectAgent">{{ $t('查看 Agent 公钥') }}</Button>
        <p v-for="identity in agentIdentities" :key="identity.fingerprint" class="text-xs break-all select-text">{{ identity.keyType }} · {{ identity.fingerprint }}</p>
        <label class="flex items-center gap-2 text-sm"><input v-model="form.identitiesOnly" type="checkbox" />{{ $t('仅使用指定身份（IdentitiesOnly）') }}</label>
      </div>

      <div class="server-advanced">
        <button type="button" class="server-advanced-toggle" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen">
          <Icon name="chevron-down" :size="14" extra-class="advanced-chevron" />
          <span>{{ $t('高级设置') }}</span>
          <span class="server-advanced-hint">{{ $t('跳板机 · 主机密钥别名 · known_hosts') }}</span>
          <span v-if="!advancedOpen && configuredAdvanced" class="server-advanced-badge">{{ $t('已配置') }}</span>
        </button>
        <div v-show="advancedOpen" class="server-advanced-body space-y-3">
          <label class="flex items-center gap-2 text-sm"><input v-model="form.jumpServerMode" type="checkbox" data-testid="jumpserver-mode" />JumpServer 模式</label>
          <p class="text-xs text-mist">连接 JumpServer 堡垒机时开启：终端与 SFTP 目录独立，请在 SFTP 中选择资产和账号后上传。与 SSH 跳板机配置独立，保存后重新连接生效。</p>
          <Field :label="$t('跳板机连接路径（最多 5 级）')">
            <div v-for="(jump, index) in form.jumpServerIds" :key="index" class="flex flex-wrap items-center gap-2 mb-2">
              <SearchSelect
                class="flex-1 basis-40"
                :model-value="jump"
                :aria-label="t('第 {0} 级跳板机', [index + 1])"
                :options="[{value: '', label: t('请选择跳板机')}, ...servers.servers.filter(s => s.id !== form.id).map(s => ({value: s.id, label: `${s.name} · ${s.host}`, keywords: `${s.user} ${s.group}`}))]"
                @update:model-value="value => { if (form.jumpServerIds) form.jumpServerIds[index] = value }"
              />
              <Button size="sm" :disabled="index === 0" @click="() => { const items = form.jumpServerIds!; [items[index - 1], items[index]] = [items[index]!, items[index - 1]!] }">{{ $t('上移') }}</Button>
              <Button size="sm" @click="form.jumpServerIds?.splice(index, 1)">{{ $t('删除') }}</Button>
            </div>
            <Button size="sm" :disabled="(form.jumpServerIds?.length ?? 0) >= 5" @click="(form.jumpServerIds ??= []).push('')">{{ $t('添加下一跳') }}</Button>
            <p class="text-xs text-mist mt-2">{{ $t('本机 →') }} {{ (form.jumpServerIds ?? []).map(id => servers.servers.find(s => s.id === id)?.name ?? t('请选择')).concat(form.name || t('目标服务器')).join(' → ') }}</p>
          </Field>
          <Field :label="$t('主机密钥别名（可选）')"><Input v-model="form.hostKeyAlias" /></Field>
          <Field :label="$t('额外 known_hosts 文件（每行一个，只读）')"><Textarea :model-value="(form.knownHostsSources ?? []).join('\n')" @update:model-value="value => form.knownHostsSources = String(value).split('\n').filter(Boolean)" /></Field>
        </div>
      </div>

      <div v-if="form.authType === 'password'" class="space-y-3">
        <Field :label="$t('密码 *')" html-for="server-password">
          <Input id="server-password" v-model="form.password" type="password" :placeholder="$t('登录密码')" autocomplete="current-password" />
        </Field>
        <Field v-if="credentials.list.length" :label="$t('使用已保存凭证（可选）')" html-for="server-credential">
          <SearchSelect
            id="server-credential"
            v-model="selectedCred"
            :options="[{value: '', label: t('— 不使用 —')}, ...credentials.list.map(c => ({value: c.id, label: credentialLabel(c)}))]"
            @update:model-value="applyCredential"
          />
          <p class="text-[12px] text-slate-500">{{ $t('私钥类凭证同样可选：会带出用户名与认证方式，私钥内容不会显示在界面上。') }}</p>
        </Field>
      </div>

      <div v-else-if="form.authType === 'privateKey'" class="space-y-3">
        <div>
          <span class="field-label">{{ $t('私钥来源') }}</span>
          <div class="mt-1 flex gap-2">
            <Button
              class="flex-1"
              :variant="keySource === 'file' ? 'secondary' : 'ghost'"
              :aria-pressed="keySource === 'file'"
              @click="setKeySource('file')"
            >{{ $t('密钥文件') }}</Button>
            <Button
              class="flex-1"
              :variant="keySource === 'content' ? 'secondary' : 'ghost'"
              :aria-pressed="keySource === 'content'"
              @click="setKeySource('content')"
            >{{ $t('粘贴内容') }}</Button>
          </div>
        </div>

        <Field v-if="keySource === 'file'" :label="$t('私钥文件 *')" html-for="server-key-path">
          <div class="flex gap-2">
            <Input id="server-key-path" v-model="form.keyPath" readonly class="flex-1" placeholder="~/.ssh/id_rsa" />
            <Button variant="secondary" size="sm" @click="pickKeyFile">{{ $t('选择…') }}</Button>
          </div>
        </Field>

        <div v-else>
          <span class="field-label">{{ $t('私钥内容 *') }}</span>
          <div
            v-if="usingStoredKeyContent"
            class="mt-1 rounded-md border border-[var(--field-border)] bg-[var(--hover)] px-3 py-3 text-[12px] leading-relaxed text-slate-400"
          >
            <div class="flex items-center gap-2 text-[var(--foreground)]">
              <Icon name="lock" :size="13" />{{ $t('已使用保存的私钥') }}</div>
            <p class="mt-1">{{ $t('私钥内容保存在凭据库中，出于安全考虑不在界面上回显；保存后会自动写入该服务器。\n              需要换成别的私钥时，切换上方「密钥文件」，或重新选择 / 粘贴内容。') }}</p>
          </div>
          <Textarea
            v-else
            v-model="form.keyContent"
            rows="7"
            spellcheck="false"
            autocomplete="off"
            class="mt-1 font-mono"
            :aria-label="$t('私钥内容')"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
            @input="clearCredentialRef"
          />
        </div>

        <Field v-if="credentials.list.length" :label="$t('使用已保存凭证（可选）')" html-for="server-key-credential">
          <SearchSelect
            id="server-key-credential"
            v-model="selectedCred"
            :options="[{value: '', label: t('— 不使用 —')}, ...credentials.list.map(c => ({value: c.id, label: credentialLabel(c)}))]"
            @update:model-value="applyCredential"
          />
        </Field>

        <Field :label="$t('私钥口令（可选）')" html-for="server-key-passphrase">
          <Input id="server-key-passphrase" v-model="form.password" type="password" :placeholder="$t('加密私钥的 passphrase')" autocomplete="current-password" />
        </Field>
      </div>

      <p v-if="errorMsg" class="text-xs text-rose-400 break-all" role="alert">{{ errorMsg }}</p>
      <p v-else-if="testMsg" class="text-xs break-all" :class="testOk ? 'text-[var(--success)]' : 'text-rose-400'" role="status">
        <Icon :name="testOk ? 'check' : 'alert'" :size="13" />
        {{ testMsg }}
      </p>
    </div>

    <template #footer>
      <Button variant="ghost" :disabled="saving || testing" @click="close">{{ $t('取消') }}</Button>
      <Button variant="ghost" :loading="testing" :disabled="testing || saving" @click="testConnection">
        <Icon v-if="!testing" name="activity" :size="14" />
        {{ testing ? t('测试中…') : t('测试连接') }}
      </Button>
      <Button variant="ghost" :loading="saving" :disabled="saving || testing" @click="save">
        {{ saving ? t('保存中…') : t('保存') }}
      </Button>
      <Button variant="primary" :loading="saving" :disabled="saving || testing" @click="saveAndConnect">
        <Icon v-if="!saving" name="check" :size="14" />{{ $t('保存并连接') }}</Button>
    </template>
  </Dialog>
</template>

<style scoped>
.server-advanced { border-top: 1px solid var(--line); }
.server-advanced-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 2px;
  font-size: 12.5px;
  color: var(--foreground-secondary);
  text-align: left;
}
.server-advanced-toggle:hover { color: var(--foreground); }
.advanced-chevron { color: var(--muted-foreground); transform: rotate(-90deg); transition: transform 160ms ease; }
.server-advanced-toggle[aria-expanded='true'] .advanced-chevron { transform: rotate(0deg); }
.server-advanced-hint { margin-left: auto; font-size: 11.5px; color: var(--muted-foreground); }
.server-advanced-badge {
  flex-shrink: 0;
  padding: 1px 7px;
  border-radius: var(--radius-pill);
  font-size: 10.5px;
  color: var(--accent-bg);
  background: var(--primary-weak);
}
.server-advanced-body { padding-bottom: 4px; }
</style>
