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
