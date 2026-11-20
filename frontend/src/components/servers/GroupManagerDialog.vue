<script setup lang="ts">
import {t as $t} from '../../i18n'
import {computed, ref, watch} from 'vue'
import {Button, Dialog, Input} from '../ui'
import Icon from '../Icon.vue'
import {groupParent, type GroupNode} from '../../utils/groupTree'

const props = defineProps<{
  modelValue: boolean
  tree: GroupNode[]
  createGroup: (parentPath: string, name: string) => Promise<string>
  renameGroup: (path: string, newLeaf: string) => Promise<string>
  deleteGroup: (path: string, keepChildren: boolean) => Promise<string>
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const open = computed({
  get: () => props.modelValue,
  set: (next: boolean) => emit('update:modelValue', next),
})

function flatten(nodes: GroupNode[], out: GroupNode[] = []): GroupNode[] {
  for (const node of nodes) {
    out.push(node)
    flatten(node.children, out)
  }
  return out
}

const rows = computed(() => flatten(props.tree))

const rootValue = ref('')
const rootError = ref('')
const rootSubmitting = ref(false)

const addParent = ref<string | null>(null)
const addValue = ref('')
const addError = ref('')
const adding = ref(false)

const editPath = ref('')
const editValue = ref('')
const editError = ref('')
const renaming = ref(false)

const deletePath = ref('')
const deleteKeepChildren = ref(false)
const deleteError = ref('')
const deleting = ref(false)

watch(
  () => props.modelValue,
  (visible) => {
    if (!visible) return
    rootValue.value = ''
    rootError.value = ''
    addParent.value = null
    addValue.value = ''
    addError.value = ''
    editPath.value = ''
    editValue.value = ''
    editError.value = ''
    deletePath.value = ''
    deleteError.value = ''
  },
  {immediate: true},
)

async function submitRoot() {
  if (rootSubmitting.value) return
  rootError.value = ''
  rootSubmitting.value = true
  rootError.value = await props.createGroup('', rootValue.value)
  rootSubmitting.value = false
  if (!rootError.value) rootValue.value = ''
}

function startAdd(parent: string) {
  addParent.value = parent
  addValue.value = ''
  addError.value = ''
  editPath.value = ''
  deletePath.value = ''
}

async function submitAdd() {
  if (adding.value || addParent.value === null) return
  adding.value = true
  addError.value = await props.createGroup(addParent.value, addValue.value)
  adding.value = false
  if (!addError.value) {
    addParent.value = null
    addValue.value = ''
  }
}

function cancelAdd() {
  addParent.value = null
  addValue.value = ''
  addError.value = ''
}

function startEdit(node: GroupNode) {
  editPath.value = node.path
  editValue.value = node.name
  editError.value = ''
  addParent.value = null
  deletePath.value = ''
}

async function submitEdit() {
  if (renaming.value || !editPath.value) return
  renaming.value = true
  editError.value = await props.renameGroup(editPath.value, editValue.value)
  renaming.value = false
  if (!editError.value) editPath.value = ''
}

function cancelEdit() {
  editPath.value = ''
  editValue.value = ''
  editError.value = ''
}

function startDelete(node: GroupNode) {
  deletePath.value = node.path
  deleteKeepChildren.value = false
  deleteError.value = ''
  editPath.value = ''
  addParent.value = null
}

async function submitDelete() {
  if (deleting.value || !deletePath.value) return
  deleting.value = true
  deleteError.value = await props.deleteGroup(deletePath.value, deleteKeepChildren.value)
  deleting.value = false
  if (!deleteError.value) deletePath.value = ''
}

function cancelDelete() {
  deletePath.value = ''
  deleteError.value = ''
}

function nodeCountLabel(node: GroupNode): string {
  return node.children.length
    ? $t('{0} 台 · {1} 个子分组', [String(node.total), String(node.children.length)])
    : $t('{0} 台', [String(node.total)])
}
</script>

<template>
  <Dialog v-model="open" :title="$t('分组管理')" :width="560">
    <div class="manager">
      <div class="root-create">
        <Input
          v-model="rootValue"
          :aria-label="$t('新建分组')"
          :placeholder="$t('新建分组，支持 父组/子组')"
          @keyup.enter="submitRoot"
        />
        <Button variant="primary" size="sm" :disabled="rootSubmitting || !rootValue.trim()" @click="submitRoot">
          {{ rootSubmitting ? $t('保存中…') : $t('添加') }}
        </Button>
      </div>
      <p v-if="rootError" class="error" role="alert">{{ rootError }}</p>
      <p class="hint">{{ $t('每一行右侧的 + 可在该分组下新建子分组；重命名只改当前层。') }}</p>

      <div v-if="!rows.length" class="empty">{{ $t('暂无分组，服务器节点使用的分组会自动出现。') }}</div>
      <div v-else class="rows">
        <div
          v-for="node in rows"
          :key="node.path"
          class="row"
          :style="{'--level': node.depth - 1}"
        >
          <div class="row-main">
            <Icon name="folder" :size="14" extra-class="row-folder" />
            <span class="row-name" :title="node.path">{{ node.name }}</span>
            <span class="row-count" :title="nodeCountLabel(node)">{{ node.total }}</span>
            <span class="row-actions">
              <button type="button" class="row-action" :title="$t('新建子分组')" :aria-label="$t('新建子分组')" @click="startAdd(node.path)"><Icon name="folder-plus" :size="13" /></button>
              <button type="button" class="row-action" :title="$t('重命名')" :aria-label="$t('重命名')" @click="startEdit(node)"><Icon name="pencil" :size="13" /></button>
              <button type="button" class="row-action danger" :title="$t('删除')" :aria-label="$t('删除')" @click="startDelete(node)"><Icon name="trash" :size="13" /></button>
            </span>
          </div>

          <div v-if="editPath === node.path" class="inline-editor">
            <span class="prefix">{{ groupParent(node.path) ? `${groupParent(node.path)} /` : '' }}</span>
            <Input v-model="editValue" :aria-label="$t('重命名分组 {0}', [node.path])" class="flex-1" @keyup.enter="submitEdit" />
            <Button variant="primary" size="sm" :disabled="renaming" @click="submitEdit">{{ $t('确定') }}</Button>
            <Button variant="ghost" size="sm" @click="cancelEdit">{{ $t('取消') }}</Button>
          </div>
          <p v-if="editPath === node.path && editError" class="error">{{ editError }}</p>

          <div v-if="addParent === node.path" class="inline-editor">
            <span class="prefix">{{ node.path }} /</span>
            <Input v-model="addValue" :aria-label="$t('新分组名称')" :placeholder="$t('支持 父组/子组')" class="flex-1" @keyup.enter="submitAdd" />
            <Button variant="primary" size="sm" :disabled="adding || !addValue.trim()" @click="submitAdd">{{ $t('确定') }}</Button>
            <Button variant="ghost" size="sm" @click="cancelAdd">{{ $t('取消') }}</Button>
          </div>
          <p v-if="addParent === node.path && addError" class="error">{{ addError }}</p>

          <div v-if="deletePath === node.path" class="delete-panel">
            <p class="delete-title">{{ $t('删除「{0}」？', [node.path]) }}</p>
            <p class="delete-meta">{{ $t('包含 {0} 台服务器、{1} 个子分组。', [String(node.total), String(node.children.length)]) }}</p>
            <label class="radio">
              <input v-model="deleteKeepChildren" type="radio" :value="false" />
              <span>{{ $t('删除分组及其子分组，服务器变为未分组') }}</span>
            </label>
            <label v-if="node.children.length" class="radio">
              <input v-model="deleteKeepChildren" type="radio" :value="true" />
              <span>{{ $t('仅删除此分组，子分组上移一级、直属服务器移到上一级') }}</span>
            </label>
            <p v-if="deleteError" class="error">{{ deleteError }}</p>
            <div class="delete-actions">
              <Button variant="ghost" size="sm" :disabled="deleting" @click="cancelDelete">{{ $t('取消') }}</Button>
              <Button variant="danger" size="sm" :disabled="deleting" @click="submitDelete">{{ deleting ? $t('删除中…') : $t('删除') }}</Button>
            </div>
          </div>
        </div>
      </div>
    </div>
    <template #footer>
      <Button variant="ghost" @click="open = false">{{ $t('关闭') }}</Button>
    </template>
  </Dialog>
</template>

<style scoped>
.manager {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 0;
}
.root-create {
  display: flex;
  gap: 8px;
}
.hint {
  margin: 0;
  color: var(--muted-foreground);
  font-size: 11.5px;
  line-height: 1.5;
}
.error {
  margin: 0;
  color: var(--destructive);
  font-size: 11.5px;
}
.empty {
  padding: 18px 8px;
  text-align: center;
  color: var(--muted-foreground);
  font-size: 12px;
}
.rows {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 52vh;
  overflow-y: auto;
  padding-right: 2px;
}
.row {
  padding-left: calc(var(--level, 0) * 14px);
}
.row-main {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 30px;
  padding: 0 6px;
  border-radius: var(--radius-small);
  color: var(--foreground-secondary);
  font-size: 12.5px;
}
.row-main:hover {
  background: var(--interactive-hover-bg);
  color: var(--foreground);
}
.row-folder {
  color: var(--muted-foreground);
}
.row-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.row-count {
  min-width: 18px;
  height: 17px;
  padding: 0 5px;
  display: grid;
  place-items: center;
  border-radius: var(--radius-small);
  background: var(--hover);
  color: var(--muted-foreground);
  font-family: var(--font-mono);
  font-size: 10px;
}
.row-actions {
  display: inline-flex;
  gap: 1px;
  opacity: 0.45;
}
.row-main:hover .row-actions,
.row-main:focus-within .row-actions {
  opacity: 1;
}
.row-action {
  width: 20px;
  height: 20px;
  display: grid;
  place-items: center;
  border-radius: var(--radius-small);
  color: var(--muted-foreground);
}
.row-action:hover {
  color: var(--foreground);
  background: var(--hover-strong);
}
.row-action.danger:hover {
  color: var(--destructive);
}
.inline-editor {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 6px 7px calc(28px + var(--level, 0) * 14px);
}
.prefix {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted-foreground);
  font-size: 11.5px;
}
.delete-panel {
  margin: 4px 0 6px calc(28px + var(--level, 0) * 14px);
  padding: 9px 10px;
  border-radius: var(--radius-small);
  background: var(--destructive-weak);
  box-shadow: inset 0 0 0 1px var(--destructive-border);
}
.delete-title {
  margin: 0 0 2px;
  color: var(--foreground);
  font-size: 12.5px;
  font-weight: 500;
}
.delete-meta {
  margin: 0 0 6px;
  color: var(--muted-foreground);
  font-size: 11.5px;
}
.radio {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 3px 0;
  color: var(--foreground-secondary);
  font-size: 11.5px;
  line-height: 1.4;
}
.radio input {
  margin-top: 2px;
}
.delete-actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 8px;
}
</style>
