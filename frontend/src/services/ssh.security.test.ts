import {beforeEach, describe, expect, it, vi} from 'vitest'
import type {ServerNode} from '../types'
vi.mock('../../wailsjs/go/main/App', async importOriginal => ({...await importOriginal<object>(), Connect: vi.fn()}))
vi.mock('./passphrase', () => ({requestPassphrase: vi.fn()}))
import {Connect} from '../../wailsjs/go/main/App'
import {requestPassphrase} from './passphrase'
vi.mock('./hostTrust', () => ({requestHostTrust: vi.fn()}))
import {requestHostTrust} from './hostTrust'
import {sshService} from './ssh'
const node: ServerNode = {id: 'n', name: 'host', group: '', host: 'example.org', port: 22, user: 'user', authType: 'agent', bgImage: '', blurAmount: 0, envVars: {}}
const connected = {sessionId: 's', server: 'host'}
let connect: ReturnType<typeof vi.fn>
let confirm: ReturnType<typeof vi.fn>
beforeEach(() => {
 vi.clearAllMocks()
 connect = vi.fn()
 confirm = vi.fn()
 ;(window as any).go = {main: {App: {ConnectAuthenticated: connect, ResolveHostTrust: confirm}}}
})
describe('SSH security UI boundaries', () => {
 it('does not retry or save trust when confirmation is cancelled', async () => {
  const challenge = btoa(JSON.stringify({host: 'example.org:22', key: 'public-key'})).replace(/=/g, '')
  connect.mockRejectedValue(new Error('SSH_HOST_TRUST:' + challenge))
  vi.mocked(requestHostTrust).mockResolvedValue(false)
  confirm.mockResolvedValue(undefined)
  await expect(sshService.connect('s', node, 80, 24)).rejects.toThrow('SSH_HOST_TRUST')
  expect(connect).toHaveBeenCalledTimes(1)
  expect(requestHostTrust).toHaveBeenCalledTimes(1)
 })
 it('never offers confirmation for a changed key', async () => {
  connect.mockRejectedValue(new Error('主机密钥发生变化，拒绝连接'))
  await expect(sshService.connect('s', node, 80, 24)).rejects.toThrow('主机密钥发生变化')
  expect(confirm).not.toHaveBeenCalled()
 })
 it('background connections bypass prompts and retain backend checks', async () => {
  vi.mocked(Connect).mockRejectedValue(new Error('unknown host'))
  await expect(sshService.connect('s', node, 80, 24, false)).rejects.toThrow('unknown host')
  expect(confirm).not.toHaveBeenCalled()
  expect(connect).not.toHaveBeenCalled()
 })
 it('keeps requested key phrases out of the saved node and clears transient parameters', async () => {
  const path = '/tmp/key'
  connect.mockRejectedValueOnce(new Error('SSH_KEY_PASSPHRASE:' + btoa(path).replace(/=/g, ''))).mockResolvedValueOnce(connected)
  vi.mocked(requestPassphrase).mockResolvedValue('test-phrase')
  await expect(sshService.connect('s', node, 80, 24)).resolves.toEqual(connected)
  expect(requestPassphrase).toHaveBeenCalledWith(path)
  expect(node.password).toBeUndefined()
  expect(connect.mock.calls[1]![4]).toEqual({})
 })
})
