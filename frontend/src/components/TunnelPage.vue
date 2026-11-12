<script lang="ts" setup>
import Icon from './Icon.vue'
import TunnelCard from './tunnels/TunnelCard.vue'
import TunnelModePicker from './tunnels/TunnelModePicker.vue'
import SearchSelect from './ui/select/SearchSelect.vue'
import {Button, Dialog, EmptyState, ErrorState, Field, Input, Select} from './ui'

import {useTunnelManagement} from '../composables/useTunnelManagement'
const {servers, tunnels, loading, busy, error, showDialog, editing, copiedId, localIPv4, loadingLocalIPv4, localIPv4Error, pendingActions, form, listenIPOptions, modeDetails, currentModeDetail, isReverseDynamic, isFixedForward, hint, refresh, submit, openCreate, openEdit, closeDialog, restartTunnel, stopTunnel, removeTunnel, filteredTunnels, tunnelAddress, addressLabel, tunnelTarget, loadLocalIPv4, copyOptions, copyAddress} = useTunnelManagement()
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <div class="page-pad" data-ui="tunnels">
      <div class="page-hero flex items-center justify-between gap-6 mb-6">
        <p class="text-[12.5px] text-mist leading-relaxed max-w-[620px]">
          通过 SSH 安全转发端口，在本机、远端服务器和目标服务之间建立访问通道。
        </p>
        <div class="flex gap-2 shrink-0">
          <Button variant="ghost" data-testid="tunnel-refresh" @click="refresh">
            <Icon name="refresh" :size="14" />
            刷新
          </Button>
          <Button variant="primary" :disabled="!servers.servers.length" data-testid="tunnel-create" @click="openCreate">
            <Icon name="plus" :size="15" />
            新建隧道
          </Button>
        </div>
      </div>

      <TunnelModePicker v-model="form.mode" :details="modeDetails" />

      <div class="neo-flat px-4 py-3 mb-5">
        <p class="text-[13px] text-[var(--foreground)]">{{ currentModeDetail.description }}</p>
        <p class="text-[12px] text-mist mt-1.5 leading-relaxed">{{ currentModeDetail.example }}</p>
        <p v-if="isReverseDynamic" class="text-[12px] text-danger mt-2">
          此代理无认证。服务器上的其他用户和进程也可能借此访问本机和本机可达的内网。
          仅连接可信服务器。OpenSSH 请使用 GatewayPorts no，并通过防火墙阻止外部访问代理端口。
        </p>
      </div>

      <ErrorState
        v-if="error && !showDialog"
        class="mb-5"
        title="隧道操作失败"
        :description="error"
        @retry="refresh"
      />

      <div v-if="filteredTunnels.length" class="grid gap-4 mb-6" style="grid-template-columns: repeat(auto-fill, minmax(300px, 1fr))">
        <TunnelCard
          v-for="t in filteredTunnels"
          :key="t.id"
          :tunnel="t"
          :address="tunnelAddress(t)"
          :address-label="addressLabel(t)"
          :target="tunnelTarget(t)"
          :copy-options="copyOptions(t)"
          :copied="copiedId === t.id"
          :busy="pendingActions.has(t.id)"
          @load-addresses="loadLocalIPv4"
          @copy="address => copyAddress(t, address)"
          @start="restartTunnel(t.id)"
          @stop="stopTunnel(t.id)"
          @restart="restartTunnel(t.id)"
          @edit="openEdit(t)"
          @remove="removeTunnel(t.id)"
        />
      </div>
      <EmptyState
        v-else-if="!loading"
        class="py-10"
        title="暂无此类隧道"
        :description="servers.servers.length ? '点击右上角「新建隧道」创建一条转发规则，或切换类型查看。' : '请先新建 SSH 服务器，再创建隧道。'"
      />
    </div>

    <Dialog v-model="showDialog" :title="editing ? '编辑隧道' : '新建隧道'" :width="600" :close-disabled="busy" @close="closeDialog">
      <div class="flex flex-col gap-4">
        <p class="text-[12px] text-mist">
          {{ editing ? '保存后按新配置生效；运行中的隧道会自动重启。' : '选择转发方式，并配置监听端口与目标地址。' }}
        </p>

        <div>
          <span class="field-label">隧道类型</span>
          <div class="seg mt-1">
            <button type="button" :class="form.mode === 'local' ? 'active' : ''" :aria-pressed="form.mode === 'local'" @click="form.mode = 'local'">本地 -L</button>
            <button type="button" :class="form.mode === 'remote' ? 'active' : ''" :aria-pressed="form.mode === 'remote'" @click="form.mode = 'remote'">远程 -R</button>
            <button type="button" :class="form.mode === 'dynamic' ? 'active' : ''" :aria-pressed="form.mode === 'dynamic'" @click="form.mode = 'dynamic'">SOCKS5 -D</button>
            <button type="button" :class="isReverseDynamic ? 'active' : ''" :aria-pressed="isReverseDynamic" @click="form.mode = 'reverse-dynamic'">反向 SOCKS5</button>
          </div>
          <div class="neo-flat px-3 py-2.5 mt-2">
            <p class="text-[12px] text-[var(--foreground)]">{{ currentModeDetail.description }}</p>
            <p class="text-[12px] text-mist mt-1">{{ currentModeDetail.example }}</p>
            <p v-if="isReverseDynamic" class="text-[12px] text-danger mt-2">
              此代理无认证。服务器上的其他用户和进程也可能借此访问本机和本机可达的内网。
              仅连接可信服务器。OpenSSH 请使用 GatewayPorts no，并通过防火墙阻止外部访问代理端口。
            </p>
          </div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <Field label="名称" html-for="tunnel-name">
            <Input id="tunnel-name" v-model="form.name" placeholder="例如：MySQL 本地入口" />
          </Field>
          <Field label="SSH 服务器" html-for="tunnel-server">
            <SearchSelect
              id="tunnel-server"
              v-model="form.serverId"
              placeholder="选择 SSH 服务器"
              :options="servers.servers.map(s => ({value: s.id, label: s.name === s.host ? s.name : `${s.name} · ${s.host}`, keywords: `${s.user} ${s.group}`}))"
            />
          </Field>
        </div>

        <Field v-if="form.mode === 'local'" label="监听范围" html-for="tunnel-listen-scope">
          <Select id="tunnel-listen-scope" v-model="form.listenScope">
            <option value="loopback">仅本机（127.0.0.1）</option>
            <option value="specific">指定本机 IP</option>
            <option value="all-ipv4">所有 IPv4 网卡（0.0.0.0）</option>
          </Select>
          <p v-if="form.listenScope !== 'loopback'" class="mt-2 text-xs text-mist">
            可达此端口的设备无需登录 SSH 即可使用转发。请在本机防火墙按来源 IP 限制访问。
          </p>
        </Field>
        <Field v-if="form.mode === 'local' && form.listenScope === 'specific'" label="本机网卡 IP" html-for="tunnel-listen-ip">
          <div class="flex items-start gap-2">
            <SearchSelect
              id="tunnel-listen-ip"
              v-model="form.listenIP"
              class="font-mono"
              placeholder="选择或搜索本机 IPv4"
              :options="listenIPOptions"
              :disabled="loadingLocalIPv4 || !listenIPOptions.length"
            />
            <Button size="sm" variant="ghost" :disabled="loadingLocalIPv4" @click="loadLocalIPv4">刷新网卡</Button>
          </div>
          <p v-if="loadingLocalIPv4" class="mt-1 text-xs text-mist">正在读取网卡地址…</p>
          <p v-else-if="localIPv4Error" class="mt-1 text-xs text-danger">读取网卡地址失败，请刷新重试。</p>
          <p v-else-if="!localIPv4.length" class="mt-1 text-xs text-mist">未发现启用的非回环 IPv4 地址。</p>
        </Field>

        <div class="grid grid-cols-2 gap-3">
          <Field
            v-if="!isReverseDynamic"
            :label="form.mode === 'remote' ? '本机服务端口' : form.mode === 'dynamic' ? '本地代理端口' : '本地监听端口'"
            html-for="tunnel-local-port"
          >
            <Input
              id="tunnel-local-port"
              :model-value="form.localPort"
              type="number"
              min="1"
              max="65535"
              class="font-mono"
              @update:model-value="form.localPort = Number($event)"
            />
          </Field>
          <Field v-if="isFixedForward" :label="form.mode === 'remote' ? '远端监听地址' : '目标主机'" html-for="tunnel-remote-host">
            <Input id="tunnel-remote-host" v-model="form.remoteHost" class="font-mono" placeholder="127.0.0.1" />
          </Field>
        </div>

        <Field v-if="form.mode !== 'dynamic'" :label="isReverseDynamic ? '服务器代理端口' : form.mode === 'remote' ? '远端监听端口' : '目标端口'" html-for="tunnel-remote-port">
          <Input
            id="tunnel-remote-port"
            :model-value="form.remotePort"
            type="number"
            min="1"
            max="65535"
            class="font-mono"
            @update:model-value="form.remotePort = Number($event)"
          />
        </Field>

        <p v-if="error" class="text-xs text-danger break-all" role="alert">{{ error }}</p>
        <p v-else class="text-[12px] text-mist">{{ hint }}</p>
      </div>

      <template #footer>
        <Button variant="ghost" :disabled="busy" @click="closeDialog">取消</Button>
        <Button variant="primary" :loading="busy" :disabled="busy" data-testid="tunnel-save" @click="submit">
          {{ busy ? (editing ? '保存中…' : '创建中…') : editing ? '保存修改' : '创建隧道' }}
        </Button>
      </template>
    </Dialog>
  </div>
</template>
