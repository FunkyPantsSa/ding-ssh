<script lang="ts" setup>
import {computed} from 'vue'
import Icon from './Icon.vue'
import ToggleSwitch from './ToggleSwitch.vue'
import {useSettingsStore} from '../stores/settings'

const settings = useSettingsStore()

const collecting = computed(() => settings.sysInfoCollectEnabled)

// 与 Go 端 internal/sshx/sysinfo.go 的 sysInfoScript 保持一致（转义展示用）。
const collectScript = [
  'echo \'---CPU---\'',
  'LC_ALL=C cat /proc/stat 2>/dev/null',
  'echo \'---MEM---\'',
  'LC_ALL=C cat /proc/meminfo 2>/dev/null',
  'echo \'---DISK---\'',
  'LC_ALL=C df -k -P 2>/dev/null',
  'echo \'---NET---\'',
  'LC_ALL=C cat /proc/net/dev 2>/dev/null',
  'echo \'---IP---\'',
  'LC_ALL=C ip -o -4 addr show 2>/dev/null || LC_ALL=C ip -4 -o addr 2>/dev/null || true',
  'echo \'---UP---\'',
  'LC_ALL=C cat /proc/uptime 2>/dev/null',
].join('\n')

async function setCollecting(v: boolean) {
  await settings.setSysInfoCollectEnabled(v)
}
</script>

<template>
  <div class="space-y-6">
    <!-- 概览 -->
    <div>
      <h3 class="text-[18px] font-semibold text-white tracking-tight">连接说明</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">
        SSH 登录一台服务器后，ding-ssh 会做两件事：打开一个交互式终端，并在必要时向远端发起少量采集请求，
        用于在底部状态栏与右侧「系统看板」展示 CPU / 内存 / 磁盘 / 网卡。下面按代码逐一说明会执行什么、采集什么，
        以及关闭自动采集后会影响哪些功能。
      </p>
    </div>

      <!-- 开关 -->
      <div class="neo">
        <div class="flex items-start justify-between gap-4 px-5 py-4">
          <div class="min-w-0">
            <p class="text-sm font-medium text-slate-200">登录后自动采集系统信息</p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">
              开启时，登录 SSH 服务器后会自动在远端采集 CPU / 内存 / 磁盘 / 网卡指标（见下方脚本）。
              <strong class="text-slate-300">关闭后，登录服务器不再向远端执行任何采集命令</strong>，
              底部状态栏与系统看板不再有数据；终端、SFTP、传输、隧道等其余功能不受影响。
            </p>
          </div>
          <ToggleSwitch
            class="mt-0.5"
            :model-value="collecting"
            @update:model-value="setCollecting"
          />
        </div>
        <div
          v-if="!collecting"
          class="px-5 py-3 border-t border-slate-800/60 flex items-start gap-2 text-[12px] text-amber-300/90 leading-relaxed"
        >
          <Icon name="info" :size="14" class="mt-0.5 shrink-0" />
          <span>
            已关闭：新建的服务器连接不会执行采集命令；已在连接的会话会立即停止采集。重新开启后，需要重新连接（或新建会话）才会恢复采集。
          </span>
        </div>
      </div>

      <!-- 自动执行的命令 -->
      <section>
        <h3 class="text-[15px] font-semibold text-white flex items-center gap-2">
          <Icon name="terminal" :size="15" class="text-signal" />
          登录后自动执行的命令
        </h3>
        <p class="text-[12px] text-mist mt-1.5 leading-relaxed">
          以下命令由 ding-ssh 在远端执行，用于系统指标采集。除采集脚本外，其余均为 SSH / SFTP 协议层的常规交互，不是额外的 shell 命令。
        </p>

        <div class="neo mt-3 overflow-hidden">
          <table class="w-full text-[12.5px]">
            <thead>
              <tr class="text-left text-mist border-b border-slate-800/60">
                <th class="font-medium px-4 py-2.5 w-[22%]">触发时机</th>
                <th class="font-medium px-4 py-2.5">执行内容</th>
              </tr>
            </thead>
            <tbody class="align-top">
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3">
                  <span class="text-slate-200">登录后立即，然后周期执行</span>
                  <span class="block text-[11px] text-mist mt-1">前台约 3 秒 / 后台约 10 秒</span>
                </td>
                <td class="px-4 py-3 text-slate-300 leading-relaxed">
                  <p class="font-mono text-[12px] text-signal">sh -c '&lt;采集脚本&gt;'</p>
                  <p class="mt-1.5 text-slate-500 leading-relaxed">
                    在独立的、<strong>不带 PTY</strong> 的 SSH 会话中运行（见下方脚本），与交互终端互不干扰。
                    用于 CPU / 内存 / 磁盘 / 网卡 / 运行时间。
                  </p>
                </td>
              </tr>
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3">
                  <span class="text-slate-200">连接期间每 15 秒</span>
                </td>
                <td class="px-4 py-3 text-slate-300 leading-relaxed">
                  <p class="font-mono text-[12px] text-signal">keepalive@openssh.com</p>
                  <p class="mt-1.5 text-slate-500 leading-relaxed">
                    SSH 协议层的保活请求，防止 NAT / 防火墙超时断线。这是网络心跳，不是 shell 命令；可在「设置 → 通用」中关闭「心跳保活」。
                  </p>
                </td>
              </tr>
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3">
                  <span class="text-slate-200">登录后一次</span>
                </td>
                <td class="px-4 py-3 text-slate-300 leading-relaxed">
                  <p class="font-mono text-[12px] text-signal">SFTP Getwd</p>
                  <p class="mt-1.5 text-slate-500 leading-relaxed">
                    读取远端当前目录，用于把提示符里的 <code class="font-mono text-slate-400">~</code> 展开为绝对路径、并让 SFTP 面板停在正确目录。这是 SFTP 协议操作，不是 shell 命令。
                  </p>
                </td>
              </tr>
              <tr>
                <td class="px-4 py-3">
                  <span class="text-slate-200">终端目录变化时（被动）</span>
                </td>
                <td class="px-4 py-3 text-slate-300 leading-relaxed">
                  <p class="font-mono text-[12px] text-signal">解析 pwd / cd 输出</p>
                  <p class="mt-1.5 text-slate-500 leading-relaxed">
                    ding-ssh 只<strong>读取</strong>终端输出中的目录信息，不主动执行命令。若开启「SFTP→终端目录同步」，在 SFTP 面板切换目录时会向终端发送一次
                    <code class="font-mono text-slate-400">cd &lt;目录&gt;</code>。
                  </p>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- 采集脚本全文 -->
      <section>
        <h3 class="text-[15px] font-semibold text-white flex items-center gap-2">
          <Icon name="file" :size="15" class="text-signal" />
          采集脚本全文（<code class="font-mono text-[12px] text-slate-400">internal/sshx/sysinfo.go</code>）
        </h3>
        <p class="text-[12px] text-mist mt-1.5 leading-relaxed">
          所有命令均使用 <code class="font-mono text-slate-400">LC_ALL=C</code> 固定区域设置，并各自带
          <code class="font-mono text-slate-400">2&gt;/dev/null</code>：单条命令失败不会中断整段脚本，也不会影响交互终端。
        </p>
        <pre
          class="neo mt-3 p-4 overflow-x-auto text-[12px] leading-relaxed font-mono text-slate-300"
        ><code>{{ collectScript }}</code></pre>
      </section>

      <!-- 采集的信息 -->
      <section>
        <h3 class="text-[15px] font-semibold text-white flex items-center gap-2">
          <Icon name="activity" :size="15" class="text-signal" />
          采集的信息
        </h3>
        <div class="neo mt-3 overflow-hidden">
          <table class="w-full text-[12.5px]">
            <thead>
              <tr class="text-left text-mist border-b border-slate-800/60">
                <th class="font-medium px-4 py-2.5 w-[22%]">段落</th>
                <th class="font-medium px-4 py-2.5">采集内容</th>
                <th class="font-medium px-4 py-2.5">用途</th>
              </tr>
            </thead>
            <tbody class="align-top">
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3 font-mono text-slate-300">CPU</td>
                <td class="px-4 py-3 text-slate-400 leading-relaxed">CPU 累计时间（user / system / idle / iowait 等）</td>
                <td class="px-4 py-3 text-slate-500 leading-relaxed">计算使用率</td>
              </tr>
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3 font-mono text-slate-300">MEM</td>
                <td class="px-4 py-3 text-slate-400 leading-relaxed">内存总量、可用、空闲、buffers / cached</td>
                <td class="px-4 py-3 text-slate-500 leading-relaxed">计算内存使用率</td>
              </tr>
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3 font-mono text-slate-300">DISK</td>
                <td class="px-4 py-3 text-slate-400 leading-relaxed">各挂载点容量、已用、百分比</td>
                <td class="px-4 py-3 text-slate-500 leading-relaxed">磁盘用量</td>
              </tr>
              <tr class="border-b border-slate-800/40">
                <td class="px-4 py-3 font-mono text-slate-300">NET</td>
                <td class="px-4 py-3 text-slate-400 leading-relaxed">各网卡收发字节计数</td>
                <td class="px-4 py-3 text-slate-500 leading-relaxed">计算上下行速率</td>
              </tr>
              <tr>
                <td class="px-4 py-3 font-mono text-slate-300">IP</td>
                <td class="px-4 py-3 text-slate-400 leading-relaxed">各网卡 IPv4 地址</td>
                <td class="px-4 py-3 text-slate-500 leading-relaxed">识别物理网卡 / 展示 IP</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- 关闭后的影响 -->
      <section>
        <h3 class="text-[15px] font-semibold text-white flex items-center gap-2">
          <Icon name="info" :size="15" class="text-signal" />
          关闭「登录后自动采集」会影响哪些功能
        </h3>
        <div class="neo mt-3">
          <div class="px-5 py-3.5 border-b border-slate-800/60">
            <p class="text-[12.5px] text-amber-300/90 font-medium">不再工作</p>
            <ul class="mt-1.5 space-y-1 text-[12.5px] text-slate-400 leading-relaxed">
              <li>· 底部状态栏的 CPU / 内存 / 磁盘 / 网卡指标（显示为占位提示）</li>
              <li>· 右侧「系统看板」的实时数据</li>
              <li>· 远端采集脚本不再执行（登录服务器后不会在远端运行任何采集命令）</li>
            </ul>
          </div>
          <div class="px-5 py-3.5">
            <p class="text-[12.5px] text-emerald-300/90 font-medium">不受影响</p>
            <ul class="mt-1.5 space-y-1 text-[12.5px] text-slate-400 leading-relaxed">
              <li>· 终端输入输出、SSH 交互会话</li>
              <li>· SFTP 文件管理与上传 / 下载</li>
              <li>· SSH 隧道（本地 / 远程 / 动态转发）</li>
              <li>· 断开自动重连、命令补全、目录同步（被动解析）</li>
            </ul>
          </div>
        </div>
      </section>

      <!-- 附注 -->
      <section>
        <h3 class="text-[15px] font-semibold text-white flex items-center gap-2">
          <Icon name="info" :size="15" class="text-signal" />
          附注
        </h3>
        <p class="mt-2 text-[12.5px] text-slate-500 leading-relaxed">
          登录一台服务器时，SSH 协议本身会启动远端登录 shell（如 bash / zsh），因此远端会执行用户自己的
          <code class="font-mono text-slate-400">.bashrc</code> / <code class="font-mono text-slate-400">.profile</code> /
          <code class="font-mono text-slate-400">motd</code> / fastfetch 等登录配置。这是 SSH 的固有行为，不属于 ding-ssh 额外执行的命令。
        </p>
      </section>
  </div>
</template>
