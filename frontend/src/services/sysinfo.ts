import {reactive} from 'vue'
import {
  SetSysInfoIdle,
  SetSysInfoRealtime,
  StartSysInfoCollector,
  StopSysInfoCollector,
} from '../../wailsjs/go/main/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import type {SysInfoSnapshot} from '../types'

export const sysInfoService = {
  start: (sessionId: string): Promise<void> => StartSysInfoCollector(sessionId),
  stop: (sessionId: string): Promise<void> => StopSysInfoCollector(sessionId),
  setIdle: (sessionId: string, idle: boolean): Promise<void> => SetSysInfoIdle(sessionId, idle),
}

// 会话级实时刷新覆盖：只在本次终端有效，不写全局设置；终端关闭由后端清理。
// 前端只记录「用户显式切换过」的会话，未切换的会话回落到全局设置。
const sessionRealtime = reactive<Record<string, boolean>>({})

/** 当前会话的有效实时刷新状态：有会话级覆盖用覆盖值，否则用全局默认。 */
export function sysInfoRealtimeFor(sessionId: string | undefined, fallback: boolean): boolean {
  if (!sessionId) return fallback
  return sessionId in sessionRealtime ? sessionRealtime[sessionId] : fallback
}

/** 切换当前会话的实时刷新（不修改全局设置）；成功后更新本地覆盖。 */
export async function setSysInfoRealtime(sessionId: string, enabled: boolean): Promise<void> {
  await SetSysInfoRealtime(sessionId, enabled)
  sessionRealtime[sessionId] = enabled
}

/** 会话结束/标签关闭时丢弃本地覆盖（后端也会清除）。 */
export function forgetSysInfoRealtime(sessionId: string): void {
  delete sessionRealtime[sessionId]
}

export function onSysInfoSnapshot(sessionId: string, handler: (snap: SysInfoSnapshot) => void): () => void {
  const event = `sysinfo:snapshot:${sessionId}`
  return EventsOn(event, (payload: SysInfoSnapshot) => handler(payload))
}
