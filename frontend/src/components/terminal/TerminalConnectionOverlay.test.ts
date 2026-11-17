import {enableAutoUnmount, mount} from '@vue/test-utils'
import {afterEach, describe, expect, it} from 'vitest'
import TerminalConnectionOverlay from './TerminalConnectionOverlay.vue'
import type {SessionTab} from '../../types'

const tab: SessionTab = {
  clientId: 'tab-1',
  sessionId: '',
  serverName: '测试服务器',
  host: '10.0.0.1',
  user: 'root',
  node: {
    id: 's1',
    name: '测试服务器',
    group: '',
    host: '10.0.0.1',
    port: 22,
    user: 'root',
    authType: 'password',
    bgImage: '',
    blurAmount: 0,
    envVars: {},
  },
  status: 'error',
  message: 'dial tcp 10.0.0.1:22: connect: no route to host',
  createdAt: 0,
  sftpPath: '/',
  kind: 'ssh',
}

const steps = {
  dns: {status: 'done' as const, logs: [], expanded: false, message: ''},
}

enableAutoUnmount(afterEach)

describe('terminal connection overlay', () => {
  it('offers editing the server from a failed connection', async () => {
    const wrapper = mount(TerminalConnectionOverlay, {
      props: {
        tab,
        settings: {autoReconnect: false},
        steps,
        connectSteps: [{key: 'dns', label: 'DNS / 直连'}],
        activeStepLabel: 'TCP 握手',
        anyStepError: true,
        autoReconnectCountdown: 0,
        autoReconnectAttempt: 0,
      },
    })

    await wrapper.get('[data-testid="terminal-edit-server"]').trigger('click')
    expect(wrapper.emitted('edit')).toHaveLength(1)
  })
})

function overlay(session: Partial<SessionTab> = {}) {
 return mount(TerminalConnectionOverlay,{props:{tab:{...tab,...session},settings:{autoReconnect:true},steps,connectSteps:[{key:'dns',label:'DNS'}],activeStepLabel:'',anyStepError:false,autoReconnectCountdown:5,autoReconnectAttempt:2}})
}
it('routes SSH retry to reconnect and local shell retry to connect', async () => {
 const remote=overlay()
 await remote.get('[data-testid="terminal-retry"]').trigger('click')
 expect(remote.emitted('reconnect')).toHaveLength(1)
 expect(remote.emitted('connect')).toBeUndefined()
 const local=overlay({kind:'local'})
 await local.get('[data-testid="terminal-retry"]').trigger('click')
 expect(local.emitted('connect')).toHaveLength(1)
 expect(local.find('[data-testid="terminal-edit-server"]').exists()).toBe(false)
})
it('supports keyboard reconnect and cancellation of the automatic retry countdown', async () => {
 const wrapper=overlay({status:'disconnected'})
 expect(wrapper.text()).toContain('5s 后自动重连')
 await wrapper.get('[tabindex="0"]').trigger('keydown',{key:'Enter'})
 expect(wrapper.emitted('reconnect')).toHaveLength(1)
 await wrapper.findAll('button').find(button => button.text()==='取消自动重连')!.trigger('click')
 expect(wrapper.emitted('cancelAutoReconnect')).toHaveLength(1)
 await wrapper.setProps({autoReconnectCountdown:0})
 expect(wrapper.text()).not.toContain('取消自动重连')
})
it('does not expose retry while connecting and removes the overlay once connected', async () => {
 const wrapper=overlay({status:'connecting'})
 expect(wrapper.find('[data-testid="terminal-retry"]').exists()).toBe(false)
 await wrapper.get('[data-testid="terminal-close-session"]').trigger('click')
 expect(wrapper.emitted('close')).toHaveLength(1)
 await wrapper.setProps({tab:{...tab,status:'connected'}})
 expect(wrapper.find('[tabindex="0"]').exists()).toBe(false)
})
