import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'

const bridge = vi.hoisted(() => ({handler: undefined as undefined | ((p: unknown) => Promise<void>), reply: vi.fn()}))
vi.mock('../../wailsjs/runtime/runtime', async importOriginal => ({
  ...await importOriginal<typeof import('../../wailsjs/runtime/runtime')>(),
  EventsOn: vi.fn((name: string, fn: (p: unknown) => Promise<void>) => {if (name === 'debug:request') bridge.handler = fn; return () => {}}),
}))
vi.mock('../../wailsjs/go/main/App', async importOriginal => ({
  ...await importOriginal<typeof import('../../wailsjs/go/main/App')>(), DebugReply: bridge.reply,
}))
import {installDebugBridge} from './bridge'

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
