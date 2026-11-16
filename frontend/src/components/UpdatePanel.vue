<script setup lang="ts">
import {ref,onMounted,onBeforeUnmount} from 'vue'
import {GetUpdateChannel,CheckUpdate,DownloadUpdate,InstallUpdateNow} from '../../wailsjs/go/main/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
const channel=ref(localStorage.getItem('update-channel')==='dev'?'dev':'main')
const auto=ref(localStorage.getItem('update-auto')!=='false')
const info=ref<Awaited<ReturnType<typeof CheckUpdate>>>()
const busy=ref(false), ready=ref(false),message=ref(''),progress=ref('')
const visible=ref(false)
let timer:ReturnType<typeof setInterval>|undefined
let off:(()=>void)|undefined
async function check(manual=false){if(busy.value||ready.value)return;busy.value=true;message.value='正在检查更新…';try{info.value=await CheckUpdate(channel.value);message.value=info.value.available?'发现新版本':'当前已是最新版本';if(manual||info.value.available)visible.value=true}catch(e){message.value=String(e);if(manual)visible.value=true}finally{busy.value=false}}
async function download(now:boolean){if(!info.value?.available)return;busy.value=true;message.value='正在后台下载并校验…';try{await DownloadUpdate(now);ready.value=true;message.value='已下载并校验，正常退出后自动安装';if(now)await InstallUpdateNow()}catch(e){message.value=String(e)}finally{busy.value=false}}
function change(){localStorage.setItem('update-channel',channel.value);info.value=undefined;message.value='';}
function autoChange(){localStorage.setItem('update-auto',String(auto.value))}
function open(){visible.value=true}
onMounted(async ()=>{if(!localStorage.getItem('update-channel')){try{channel.value=await GetUpdateChannel()}catch{}};window.addEventListener('open-updates',open);off=EventsOn('update:progress',(p:{received:number,total:number})=>{progress.value=p.total>0?`${Math.round(p.received/p.total*100)}%`:`${(p.received/1048576).toFixed(1)} MB`});if(auto.value)void check();timer=setInterval(()=>{if(auto.value)void check()},60*60*1000)})
onBeforeUnmount(()=>{window.removeEventListener('open-updates',open);off?.();clearInterval(timer)})
</script>
<template>
 <div v-if="visible" class="fixed bottom-5 right-5 z-[100] w-96 rounded-xl border border-slate-600 bg-slate-900 p-5 text-slate-200 shadow-xl" role="dialog" aria-label="应用更新">
  <div class="flex justify-between"><strong>应用更新</strong><button aria-label="关闭更新面板" @click="visible=false">×</button></div>
  <label class="block mt-3">更新通道 <select v-model="channel" :disabled="busy||ready" class="bg-slate-800 p-1" @change="change"><option value="main">main</option><option value="dev">dev（测试版）</option></select></label>
  <label class="block mt-3"><input v-model="auto" type="checkbox" @change="autoChange"> 自动检查更新，确认后安装</label>
  <p class="mt-3 text-sm">{{message}} {{busy?progress:''}}</p>
  <p v-if="info" class="mt-2 text-xs break-all">{{info.manifest.commit.slice(0,12)}} · {{info.manifest.builtAt}}</p>
  <p class="mt-3 text-xs text-slate-400">下载遵循系统代理。安装会关闭 SSH 会话和文件传输，请先完成当前操作。</p>
  <div class="mt-4 flex gap-3 text-sm">
   <button :disabled="busy||ready" @click="check(true)">检查更新</button>
   <button v-if="info?.available&&!ready" :disabled="busy" @click="download(true)">立即更新</button>
   <button v-if="info?.available&&!ready" :disabled="busy" @click="download(false)">退出时更新</button>
   <button v-if="ready" @click="InstallUpdateNow()">立即重启安装</button>
  </div>
 </div>
</template>
