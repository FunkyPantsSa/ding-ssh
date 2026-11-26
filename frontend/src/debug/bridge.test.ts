import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import {useSettingsStore} from '../stores/settings'
import {confirmTabs} from '../services/closeConfirmation'
vi.mock('../services/closeConfirmation', () => ({confirmTabs: vi.fn(), needsTabConfirmation: () => {const s=useSettingsStore(); return s.loaded && s.confirmCloseTab}}))

const bridge = vi.hoisted(() => ({handler: undefined as undefined | ((p: unknown) => Promise<void>), reply: vi.fn()}))
vi.mock('../services/desktop', async importOriginal => ({
  ...await importOriginal<typeof import('../services/desktop')>(),
  EventsOn: vi.fn((name: string, fn: (p: unknown) => Promise<void>) => {if (name === 'debug:request') bridge.handler = fn; return () => {}}),
}))
vi.mock('../../bindings/ding-ssh/internal/apphost/app', async importOriginal => ({
  ...await importOriginal<typeof import('../../bindings/ding-ssh/internal/apphost/app')>(), DebugReply: bridge.reply,
}))
import {installDebugBridge} from './bridge'
import {registerDebugTerminal, unregisterDebugTerminal, type DebugTerminalEntry} from './registry'

beforeEach(() => {
  setActivePinia(createPinia())
  bridge.reply.mockReset()
  installDebugBridge()
})
async function call(op: string, args: Record<string, unknown>) {
  await bridge.handler!({id: 'test', op, args})
  return JSON.parse(bridge.reply.mock.calls.at(-1)![1])
}
describe('workspace MCP bridge', () => {
  it('returns the exact new tab identifier and describes a disconnected local tab', async () => {
    const opened=await call('tab.open',{local:true})
    const id=opened.data.sessionId
    expect(opened.data).toMatchObject({tabId:id,local:true,status:'connecting'})
    useSessionsStore().setStatus(id,'closed')
    expect((await call('session.describe',{id})).data).toMatchObject({sessionId:id,tabId:id,local:true,tabExists:true,status:'closed'})
    expect((await call('session.describe',{id:'missing'})).data).toMatchObject({tabExists:false,status:'closed'})
  })
  it('reports exact-tab confirmation and cancellation without clicking DOM', async () => {
    const settings=useSettingsStore(); settings.loaded=true; settings.confirmCloseTab=true
    const store=useSessionsStore(); const a=store.openLocalTab('audit target'), b=store.openLocalTab('keep')
    let decide: (ok:boolean)=>void=()=>{}
    vi.mocked(confirmTabs).mockImplementation(()=>new Promise(resolve=>{decide=resolve}))
    const pending=await call('tab.close',{id:a.clientId})
    expect(pending.data).toMatchObject({tabId:a.clientId,closeState:'confirmation_required',tabExists:true})
    expect(confirmTabs).toHaveBeenLastCalledWith(1,['audit target'])
    const requests=vi.mocked(confirmTabs).mock.calls.length
    expect((await call('tab.close',{id:a.clientId})).data.closeState).toBe('confirmation_required')
    expect(confirmTabs).toHaveBeenCalledTimes(requests)
    decide(false); await Promise.resolve()
    expect((await call('session.describe',{id:a.clientId})).data.closeState).toBe('cancelled')
    await call('tab.close',{id:a.clientId})
    store.activateTab(b.clientId)
    decide(true); await Promise.resolve()
    expect(store.tabs.map(t=>t.clientId)).toEqual([b.clientId])
    expect((await call('session.describe',{id:a.clientId})).data.closeState).toBe('closed')
  })
  it('distinguishes Unicode text from raw base64 input', async () => {
    const write = vi.fn()
    const entry = {clientId: 'audit-input', write} as unknown as DebugTerminalEntry
    registerDebugTerminal(entry)
    try {
      expect((await call('terminal.input', {id: entry.clientId, data: 'é中文'})).ok).toBe(true)
      expect(write).toHaveBeenLastCalledWith('é中文', false)
      expect((await call('terminal.input', {id: entry.clientId, base64: btoa('\xff\x00')})).ok).toBe(true)
      expect(write).toHaveBeenLastCalledWith('\xff\x00', true)
      expect((await call('terminal.input', {id: entry.clientId, base64: 'invalid%'})).ok).toBe(false)
    } finally { unregisterDebugTerminal(entry.clientId, entry) }
  })
  it('activates only existing tabs', async () => {
    const sessions = useSessionsStore()
    const a = sessions.openLocalTab(), b = sessions.openLocalTab()
    sessions.bindSession(a.clientId,'session-a')
    expect((await call('tabs.activate',{id:a.clientId})).ok).toBe(true)
    expect(sessions.activeId).toBe(a.clientId)
    expect(useUIStore().view).toBe('workspace')
    expect((await call('tabs.activate',{id:'missing'})).ok).toBe(false)
    expect(sessions.tabs.map(t=>t.clientId)).toEqual([a.clientId,b.clientId])
    expect(sessions.tabs[0].sessionId).toBe('session-a')
  })
  it('shows and hides panels without changing tabs', async () => {
    const sessions=useSessionsStore()
    for (const panel of ['sftp','sysinfo','transfers']) {
      expect((await call('panels.set',{panel})).ok).toBe(true)
      expect(sessions.rightPanel).toBe(panel)
      expect(sessions.sftpVisible).toBe(true)
    }
    expect((await call('panels.set',{panel:'hidden'})).ok).toBe(true)
    expect(sessions.sftpVisible).toBe(false)
    expect((await call('panels.set',{panel:'invalid'})).ok).toBe(false)
    expect(sessions.tabs).toHaveLength(0)
  })
  it('splits and restores existing tabs without replacing sessions', async () => {
    const sessions=useSessionsStore()
    const a=sessions.openLocalTab(), b=sessions.openLocalTab()
    sessions.bindSession(a.clientId,'session-a'); sessions.bindSession(b.clientId,'session-b')
    expect((await call('workspace.split',{sourceId:a.clientId,targetId:b.clientId})).ok).toBe(true)
    expect(sessions.splitShown).toBe(true)
    expect((await call('workspace.split',{enabled:false})).ok).toBe(true)
    expect(sessions.splitShown).toBe(false)
    expect(sessions.tabs.map(t=>t.sessionId)).toEqual(['session-a','session-b'])
    expect((await call('workspace.split',{sourceId:a.clientId,targetId:'missing'})).ok).toBe(false)
    expect(sessions.panes).toBeNull()
    expect((await call('workspace.split',{enabled:'false'})).ok).toBe(false)
  })
})
