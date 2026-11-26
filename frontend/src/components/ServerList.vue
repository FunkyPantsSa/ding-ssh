<script lang="ts" setup>
import {t as $t} from '../i18n'

import {t} from '../i18n'
import {ref} from 'vue'

import Icon from './Icon.vue'
import ServerDialog from './ServerDialog.vue'
import SSHConfigImportDialog from './SSHConfigImportDialog.vue'
import ServerGroupSidebar from './servers/ServerGroupSidebar.vue'
import GroupBreadcrumb from './servers/GroupBreadcrumb.vue'
import GroupContextMenu from './servers/GroupContextMenu.vue'
import GroupEditorDialog from './servers/GroupEditorDialog.vue'
import GroupManagerDialog from './servers/GroupManagerDialog.vue'
import ServerToolbar from './servers/ServerToolbar.vue'
import ServerContextMenu from './servers/ServerContextMenu.vue'
import ServerRow from './servers/ServerRow.vue'
import {Button, Dialog, EmptyState, ErrorState} from './ui'

import {useServerManagement} from '../composables/useServerManagement'
import type {ServerNode} from '../types'

const {
  servers, keyword, selection, groupFilter, groupTree, filteredGroupTree, selectedTotal, selectedDirect,
  selectedHasChildren, showDialog, showSSHImport, editing, confirmNode, serverDialogGroup, showGroupManager,
  groupEditor, groupDeleteTarget, filtered, onlineServers, openNew, openEdit, copiedHostId, copyHost, serverMenu,
  cloning, openRowMenu, closeRowMenu, menuCopyHost, menuEdit, menuTest, menuDuplicate, draggingServerId,
  groupDragOver, onRowDragStart, onRowDragEnd, onGroupDragOver, onGroupDragLeave, onGroupDrop, connect,
  nodeStatus, testBadge, isActiveNode, remove, groupMenu, openGroupMenu, closeGroupMenu, menuCreateChild,
  menuNewServer, menuRenameGroup, menuRemoveGroup, openGroupManager, openCreateGroup, closeGroupEditor,
  submitGroupEditor, createGroupAt, renameGroupAt, deleteGroupAt, requestRemoveGroup, confirmGroupDelete,
  closeGroupDelete, allCount, ungroupedCount, sidebarExpanded, toggleExpanded, collapseAll, selectGroup,
} = useServerManagement()

const groupDeleting = ref(false)

/** 在“全部服务器”视图里给每行补一个分组路径标签；进入具体分组后由面包屑提供上下文。 */
function groupLabelFor(node: ServerNode): string {
  return selection.value.kind === 'all' ? node.group.trim() : ''
}

async function confirmDeleteGroup() {
  if (groupDeleting.value) return
  groupDeleting.value = true
  try {
    await confirmGroupDelete()
  } finally {
    groupDeleting.value = false
  }
}
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <div class="page-pad flex flex-col gap-4 min-h-0">
      <!-- 页面头：说明 + 在线统计（标题已在顶栏呈现，此处不重复） -->
      <div class="page-hero flex items-center justify-between gap-6 shrink-0">
        <p class="text-[12.5px] text-mist leading-relaxed max-w-[560px]">{{ $t('添加、编辑与删除 SSH 节点，按分组组织并测试在线状态。') }}</p>
        <div class="flex items-center gap-2 shrink-0">
          <span class="chip">
            <span class="dot"></span>
            {{ onlineServers }}/{{ servers.servers.length }} {{ $t('台在线') }}</span>
        </div>
      </div>

      <div class="flex-1 min-h-0 flex gap-4">
        <!-- 左：分组树 -->
        <ServerGroupSidebar
          :tree="filteredGroupTree"
          :selection="selection"
          :drag-over="groupDragOver"
          :all-count="allCount"
          :ungrouped-count="ungroupedCount"
          :filter="groupFilter"
          :expanded="sidebarExpanded"
          @select="selectGroup"
          @toggle="toggleExpanded"
          @manage="openGroupManager"
          @create-root="openCreateGroup('')"
          @create-child="openCreateGroup()"
          @collapse-all="collapseAll"
          @update:filter="groupFilter = $event"
          @drag-over="onGroupDragOver"
          @drag-leave="onGroupDragLeave"
          @drop="onGroupDrop"
          @context-menu="openGroupMenu"
        />

        <!-- 右：列表区域 -->
        <div class="flex-1 min-w-0 flex flex-col gap-3">
          <!-- 工具栏 -->
          <ServerToolbar @import-config="showSSHImport = true" v-model="keyword" :total="servers.servers.length" :online="onlineServers" :testing="servers.testingAll" @test-all="servers.testAll()" @refresh="servers.load()" @create="openNew" />

          <!-- 列表 -->
          <div class="flex-1 min-h-0 overflow-y-auto server-list-panel" data-keyboard-nav="vertical">
            <div class="list-crumbs">
              <GroupBreadcrumb
                :selection="selection"
                :total="selectedTotal"
                :direct="selectedDirect"
                :has-children="selectedHasChildren"
                @select="selectGroup"
              >
                <template #meta><span class="text-[11px] text-mist">{{ $t('按分组路径排序') }}</span></template>
              </GroupBreadcrumb>
            </div>
            <div v-if="servers.loading" class="flex flex-col" aria-busy="true" :aria-label="$t('正在加载服务器')">
              <div v-for="i in 6" :key="i" class="flex items-center gap-3 px-4 py-3">
                <div class="skel w-2 h-2 rounded-full shrink-0"></div>
                <div class="skel h-3 w-1/3"></div>
                <div class="skel h-3 w-24 ml-auto"></div>
              </div>
            </div>
            <ErrorState
              v-else-if="servers.error"
              :title="$t('服务器列表加载失败')"
              :description="servers.error"
              @retry="servers.load()"
            />
            <EmptyState
              v-else-if="filtered.length === 0"
              :title="servers.servers.length ? t('没有匹配的服务器') : t('暂无服务器')"
              :description="servers.servers.length ? t('换个关键词，或切换左侧分组试试。') : t('点击「新建服务器」添加节点，之后即可在终端页快速连接。')"
            >
              <template #icon><Icon name="server" :size="32" extra-class="text-mist/40" /></template>
              <template v-if="!servers.servers.length" #actions>
                <Button variant="primary" size="sm" data-testid="server-create-empty" @click="openNew">
                  <Icon name="plus" :size="14" />{{ $t('新建服务器') }}</Button>
              </template>
            </EmptyState>

            <ServerRow v-for="node in filtered" :key="node.id" :node="node" :status="nodeStatus(node)" :active="isActiveNode(node)" :dragging="draggingServerId === node.id" :badge="testBadge(node)" :copied="copiedHostId === node.id" :group-label="groupLabelFor(node)" @connect="connect(node)" @copy="copyHost(node)" @test="servers.testOne(node)" @edit="openEdit(node)" @remove="confirmNode = node" @menu="openRowMenu($event, node)" @dragstart="onRowDragStart($event, node)" @dragend="onRowDragEnd" />
          </div>
        </div>
      </div>
    </div>

    <SSHConfigImportDialog v-model="showSSHImport" />
    <ServerDialog v-model="showDialog" :editing="editing" :default-group="serverDialogGroup" />

    <ServerContextMenu v-if="serverMenu" :node="serverMenu.node" :position="serverMenu" :copied="copiedHostId === serverMenu.node.id" :cloning="cloning" @edit="menuEdit" @copy="menuCopyHost" @duplicate="menuDuplicate" @test="menuTest" @remove="confirmNode = serverMenu!.node; closeRowMenu()" @close="closeRowMenu" />

    <GroupContextMenu
      v-if="groupMenu"
      :path="groupMenu.path"
      :position="groupMenu"
      @create-child="menuCreateChild"
      @new-server="menuNewServer"
      @rename="menuRenameGroup"
      @remove="menuRemoveGroup"
      @close="closeGroupMenu"
    />

    <GroupEditorDialog
      :model-value="!!groupEditor"
      :mode="groupEditor?.mode ?? 'create'"
      :parent-path="groupEditor?.parentPath ?? ''"
      :initial-value="groupEditor?.value ?? ''"
      :error="groupEditor?.error ?? ''"
      :submitting="groupEditor?.submitting ?? false"
      @update:model-value="(visible) => { if (!visible) closeGroupEditor() }"
      @submit="submitGroupEditor"
      @clear-error="groupEditor && (groupEditor.error = '')"
    />

    <GroupManagerDialog
      v-model="showGroupManager"
      :tree="groupTree"
      :create-group="createGroupAt"
      :rename-group="renameGroupAt"
      :delete-group="deleteGroupAt"
    />

    <Dialog :model-value="!!confirmNode" :title="$t('删除服务器')" :width="420" @update:model-value="(v) => { if (!v) confirmNode = null }">
      <p class="text-[13px] text-mist leading-relaxed">{{ $t('确认删除服务器「') }} {{ confirmNode?.name }} {{ $t('」？此操作不可恢复。') }}</p>
      <template #footer>
        <Button variant="ghost" @click="confirmNode = null">{{ $t('取消') }}</Button>
        <Button variant="danger" data-testid="server-delete-confirm" @click="remove">{{ $t('删除') }}</Button>
      </template>
    </Dialog>

    <Dialog
      :model-value="!!groupDeleteTarget"
      :title="$t('删除分组')"
      :width="460"
      @update:model-value="(v) => { if (!v) closeGroupDelete() }"
    >
      <template v-if="groupDeleteTarget">
        <div class="flex flex-col gap-2.5">
          <p class="text-[13px] text-[var(--foreground)] leading-relaxed">{{ $t('删除「{0}」？', [groupDeleteTarget.path]) }}</p>
          <p class="text-[12px] text-mist leading-relaxed">
            <template v-if="groupDeleteTarget.childCount">
              {{ $t('包含 {0} 台服务器、{1} 个直接子分组。', [String(groupDeleteTarget.total), String(groupDeleteTarget.childCount)]) }}
            </template>
            <template v-else>
              {{ $t('其中 {0} 台服务器将变为未分组。', [String(groupDeleteTarget.total)]) }}
            </template>
          </p>
          <label class="flex items-start gap-2 text-[12.5px] text-[var(--foreground-secondary)] leading-relaxed">
            <input v-model="groupDeleteTarget.keepChildren" type="radio" :value="false" class="mt-0.5" />
            <span>{{ $t('删除分组及其子分组，服务器变为未分组') }}</span>
          </label>
          <label v-if="groupDeleteTarget.childCount" class="flex items-start gap-2 text-[12.5px] text-[var(--foreground-secondary)] leading-relaxed">
            <input v-model="groupDeleteTarget.keepChildren" type="radio" :value="true" class="mt-0.5" />
            <span>{{ $t('仅删除此分组，子分组上移一级、直属服务器移到上一级') }}</span>
          </label>
          <p v-if="groupDeleteTarget.error" class="text-[12px] text-[var(--destructive)]" role="alert">{{ groupDeleteTarget.error }}</p>
        </div>
      </template>
      <template #footer>
        <Button variant="ghost" :disabled="groupDeleting" @click="closeGroupDelete">{{ $t('取消') }}</Button>
        <Button variant="danger" :disabled="groupDeleting" data-testid="group-delete-confirm" @click="confirmDeleteGroup">
          {{ groupDeleting ? $t('删除中…') : $t('删除') }}
        </Button>
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.list-crumbs {
  display: flex;
  align-items: center;
  height: 40px;
  padding: 0 14px;
  flex: none;
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--surface-bg);
  box-shadow: inset 0 -1px 0 var(--line);
}
</style>
