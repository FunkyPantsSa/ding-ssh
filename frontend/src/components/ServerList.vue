<script lang="ts" setup>
import Icon from './Icon.vue'
import ServerDialog from './ServerDialog.vue'
import SSHConfigImportDialog from './SSHConfigImportDialog.vue'
import ServerGroupSidebar from './servers/ServerGroupSidebar.vue'
import ServerToolbar from './servers/ServerToolbar.vue'
import ServerContextMenu from './servers/ServerContextMenu.vue'
import ServerRow from './servers/ServerRow.vue'
import {Button, Dialog, EmptyState, ErrorState, Input} from './ui'

import {useServerManagement} from '../composables/useServerManagement'
const {servers, groups, keyword, activeGroup, showDialog, showSSHImport, editing, confirmNode, showGroupManager, newGroupName, renameTarget, renameInput, confirmGroupDelete, groupError, sortedGroups, groupCount, filtered, onlineServers, openNew, openEdit, copiedHostId, copyHost, serverMenu, cloning, openRowMenu, closeRowMenu, menuCopyHost, menuEdit, menuTest, menuDuplicate, draggingServerId, groupDragOver, onRowDragStart, onRowDragEnd, onGroupDragOver, onGroupDragLeave, onGroupDrop, connect, nodeStatus, testBadge, isActiveNode, remove, openGroupManager, addGroup, startRename, doRename, removeGroup} = useServerManagement()
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <div class="page-pad flex flex-col gap-4 min-h-0">
      <!-- 页面头：说明 + 在线统计（标题已在顶栏呈现，此处不重复） -->
      <div class="page-hero flex items-center justify-between gap-6 shrink-0">
        <p class="text-[12.5px] text-mist leading-relaxed max-w-[560px]">
          添加、编辑与删除 SSH 节点，按分组组织并测试在线状态。
        </p>
        <div class="flex items-center gap-2 shrink-0">
          <span class="chip">
            <span class="dot"></span>
            {{ onlineServers }}/{{ servers.servers.length }} 台在线
          </span>
        </div>
      </div>

      <div class="flex-1 min-h-0 flex gap-4">
        <!-- 左：分组栏 -->
        <ServerGroupSidebar :groups="sortedGroups" :active-group="activeGroup" :drag-over="groupDragOver" :all-count="groupCount('all')" :ungrouped-count="groupCount('')" :count-for="groupCount" @select="activeGroup = $event" @manage="openGroupManager" @dragover="onGroupDragOver" @dragleave="onGroupDragLeave" @drop="onGroupDrop" />

        <!-- 右：列表区域 -->
        <div class="flex-1 min-w-0 flex flex-col gap-3">
          <!-- 工具栏 -->
        <ServerToolbar @import-config="showSSHImport = true" v-model="keyword" :total="servers.servers.length" :online="onlineServers" :testing="servers.testingAll" @test-all="servers.testAll()" @refresh="servers.load()" @create="openNew" />

          <!-- 列表 -->
          <div class="flex-1 min-h-0 overflow-y-auto server-list-panel">
            <div v-if="servers.loading" class="flex flex-col" aria-busy="true" aria-label="正在加载服务器">
              <div v-for="i in 6" :key="i" class="flex items-center gap-3 px-4 py-3">
                <div class="skel w-2 h-2 rounded-full shrink-0"></div>
                <div class="skel h-3 w-1/3"></div>
                <div class="skel h-3 w-24 ml-auto"></div>
              </div>
            </div>
            <ErrorState
              v-else-if="servers.error"
              title="服务器列表加载失败"
              :description="servers.error"
              @retry="servers.load()"
            />
            <EmptyState
              v-else-if="filtered.length === 0"
              :title="servers.servers.length ? '没有匹配的服务器' : '暂无服务器'"
              :description="servers.servers.length ? '换个关键词，或切换左侧分组试试。' : '点击「新建服务器」添加节点，之后即可在终端页快速连接。'"
            >
              <template #icon><Icon name="server" :size="32" extra-class="text-mist/40" /></template>
              <template v-if="!servers.servers.length" #actions>
                <Button variant="primary" size="sm" data-testid="server-create-empty" @click="openNew">
                  <Icon name="plus" :size="14" />
                  新建服务器
                </Button>
              </template>
            </EmptyState>

            <ServerRow v-for="node in filtered" :key="node.id" :node="node" :status="nodeStatus(node)" :active="isActiveNode(node)" :dragging="draggingServerId === node.id" :badge="testBadge(node)" :copied="copiedHostId === node.id" @connect="connect(node)" @copy="copyHost(node)" @test="servers.testOne(node)" @edit="openEdit(node)" @remove="confirmNode = node" @menu="openRowMenu($event, node)" @dragstart="onRowDragStart($event, node)" @dragend="onRowDragEnd" />
          </div>
        </div>
      </div>
    </div>

    <SSHConfigImportDialog v-model="showSSHImport" />
    <ServerDialog v-model="showDialog" :editing="editing" :default-group="activeGroup" />

    <ServerContextMenu v-if="serverMenu" :node="serverMenu.node" :position="serverMenu" :copied="copiedHostId === serverMenu.node.id" :cloning="cloning" @edit="menuEdit" @copy="menuCopyHost" @duplicate="menuDuplicate" @test="menuTest" @remove="confirmNode = serverMenu!.node; closeRowMenu()" @close="closeRowMenu" />

    <Dialog :model-value="!!confirmNode" title="删除服务器" :width="420" @update:model-value="(v) => { if (!v) confirmNode = null }">
      <p class="text-[13px] text-mist leading-relaxed">确认删除服务器「{{ confirmNode?.name }}」？此操作不可恢复。</p>
      <template #footer>
        <Button variant="ghost" @click="confirmNode = null">取消</Button>
        <Button variant="danger" data-testid="server-delete-confirm" @click="remove">删除</Button>
      </template>
    </Dialog>

    <Dialog v-model="showGroupManager" title="分组管理" :width="420">
      <div class="space-y-3">
        <div class="flex gap-2">
          <Input v-model="newGroupName" aria-label="新分组名称" placeholder="新分组名称" class="flex-1" @keyup.enter="addGroup" />
          <Button variant="primary" size="sm" @click="addGroup">添加</Button>
        </div>
        <p v-if="groupError" class="text-xs text-danger" role="alert">{{ groupError }}</p>
        <div v-if="!groups.list.length" class="text-xs text-mist py-2 text-center">
          暂无手动分组，服务器节点使用的分组会自动出现。
        </div>
        <div
          v-for="g in groups.list"
          :key="g"
          class="neo-flat flex items-center justify-between gap-2 px-3 py-2"
        >
          <template v-if="renameTarget === g">
            <Input v-model="renameInput" :aria-label="`重命名分组 ${g}`" class="flex-1" @keyup.enter="doRename" />
            <Button variant="primary" size="sm" @click="doRename">确定</Button>
            <Button variant="ghost" size="sm" @click="renameTarget = ''">取消</Button>
          </template>
          <template v-else>
            <span class="min-w-0 truncate text-[13px] text-[var(--foreground)]">{{ g }}</span>
            <div class="flex items-center gap-1 shrink-0">
              <template v-if="confirmGroupDelete !== g">
                <Button variant="ghost" size="sm" @click="startRename(g)">重命名</Button>
                <Button variant="ghost" size="sm" @click="confirmGroupDelete = g">删除</Button>
              </template>
              <template v-else>
                <span class="text-[12px] text-mist">删除后其中服务器将变为未分组</span>
                <Button variant="danger" size="sm" @click="removeGroup(g)">确认</Button>
                <Button variant="ghost" size="sm" @click="confirmGroupDelete = ''">取消</Button>
              </template>
            </div>
          </template>
        </div>
      </div>
    </Dialog>
  </div>
</template>
