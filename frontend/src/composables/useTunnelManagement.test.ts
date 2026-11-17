import {flushPromises, mount} from '@vue/test-utils'
import {defineComponent} from 'vue'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {useTunnelManagement} from './useTunnelManagement'
import type {TunnelInfo} from '../types'
const mocks = vi.hoisted(() => ({list:vi.fn(), start:vi.fn(), update:vi.fn(), restart:vi.fn(), stop:vi.fn(), remove:vi.fn(), off:vi.fn(), subscribe:vi.fn(), copy:vi.fn()}))
vi.mock('../stores/servers', () => ({useServersStore: () => ({servers:[{id:'s1',name:'server'}]})}))
vi.mock('../services/ssh', () => ({onTunnelStatus:mocks.subscribe, sshService:{listTunnels:mocks.list,startTunnel:mocks.start,updateTunnel:mocks.update,restartTunnel:mocks.restart,stopTunnel:mocks.stop,removeTunnel:mocks.remove}}))
vi.mock('../../wailsjs/runtime/runtime', () => ({ClipboardSetText:mocks.copy}))
let wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => {vi.resetAllMocks();mocks.list.mockResolvedValue([]);mocks.subscribe.mockReturnValue(mocks.off)})
afterEach(() => {wrappers.forEach(w => w.unmount());wrappers=[]})
async function setup() {
 let state!: ReturnType<typeof useTunnelManagement>
 const wrapper=mount(defineComponent({setup(){state=useTunnelManagement();return () => null}}))
 wrappers.push(wrapper)
 await flushPromises()
 return {state,wrapper}
}
it.each([0,65536,1.5])('rejects invalid local port %s without contacting the backend', async port => {
 const {state}=await setup();state.openCreate();state.form.localPort=port
 await state.submit()
 expect(state.error.value).toContain('本地端口无效')
 expect(mocks.start).not.toHaveBeenCalled()
})
it('retains the form after save failure and permits a successful retry', async () => {
 const {state}=await setup();state.openCreate();state.form.name='custom'
 mocks.start.mockRejectedValueOnce(new Error('authentication failed'))
 await state.submit()
 expect(state.error.value).toContain('authentication failed')
 expect(state.showDialog.value).toBe(true)
 expect(state.busy.value).toBe(false)
 expect(state.form.name).toBe('custom')
 await state.submit()
 expect(state.showDialog.value).toBe(false)
 expect(state.error.value).toBe('')
 expect(mocks.start).toHaveBeenCalledTimes(2)
})
it('deduplicates pending actions and allows retry after failure', async () => {
 const {state}=await setup()
 let reject!: (error:Error) => void
 mocks.stop.mockImplementationOnce(() => new Promise((_, fail) => {reject=fail}))
 const pending=state.stopTunnel('t1')
 await state.stopTunnel('t1')
 expect(mocks.stop).toHaveBeenCalledTimes(1)
 expect(state.pendingActions.has('t1')).toBe(true)
 reject(new Error('connection lost'));await pending
 expect(state.pendingActions.has('t1')).toBe(false)
 expect(state.error.value).toContain('connection lost')
 await state.stopTunnel('t1')
 expect(mocks.stop).toHaveBeenCalledTimes(2)
 expect(state.error.value).toBe('')
})
it('preserves edited configuration and restarts a previously running tunnel after replacement', async () => {
 const {state}=await setup()
 const tunnel={id:'t1',serverId:'s1',name:'saved',mode:'dynamic',localPort:18080,remotePort:0,listenHost:'127.0.0.1',status:'running'} as TunnelInfo
 state.openEdit(tunnel);await flushPromises()
 expect(state.form.localPort).toBe(18080)
 mocks.update.mockResolvedValue({...tunnel,status:'stopped'})
 await state.submit()
 expect(mocks.update).toHaveBeenCalledWith('t1',expect.anything(),'saved','dynamic',18080,'127.0.0.1',0,'127.0.0.1')
 expect(mocks.restart).toHaveBeenCalledWith('t1')
 expect(mocks.start).not.toHaveBeenCalled()
})
it('updates only matching tunnel status and removes the event subscription on unmount', async () => {
 const {state,wrapper}=await setup()
 state.tunnels.value=[{id:'t1',status:'running'} as TunnelInfo]
 const handler=mocks.subscribe.mock.calls[0][0]
 handler({id:'other',status:'error'})
 expect(state.tunnels.value[0].status).toBe('running')
 handler({id:'t1',status:'error',message:'port busy'})
 expect(state.tunnels.value[0]).toMatchObject({status:'error',message:'port busy'})
 wrapper.unmount();wrappers=[]
 expect(mocks.off).toHaveBeenCalledTimes(1)
})
it('surfaces clipboard failures without showing a false copied state', async () => {
 const {state}=await setup();mocks.copy.mockRejectedValueOnce(new Error('clipboard unavailable'))
 await state.copyAddress({id:'t1'} as TunnelInfo,'127.0.0.1:22')
 expect(state.copiedId.value).toBe('')
 expect(state.error.value).toContain('clipboard unavailable')
})
