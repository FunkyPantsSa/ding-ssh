import {
  SetSysInfoIdle,
  StartSysInfoCollector,
  StopSysInfoCollector,
} from '../../wailsjs/go/main/App'
import {EventsOff, EventsOn} from '../../wailsjs/runtime/runtime'
import type {SysInfoSnapshot} from '../types'

export const sysInfoService = {
  start: (sessionId: string): Promise<void> => StartSysInfoCollector(sessionId),
  stop: (sessionId: string): Promise<void> => StopSysInfoCollector(sessionId),
  setIdle: (sessionId: string, idle: boolean): Promise<void> => SetSysInfoIdle(sessionId, idle),
}

export function onSysInfoSnapshot(sessionId: string, handler: (snap: SysInfoSnapshot) => void): () => void {
  const event = `sysinfo:snapshot:${sessionId}`
  EventsOn(event, (payload: SysInfoSnapshot) => handler(payload))
  return () => EventsOff(event)
}
