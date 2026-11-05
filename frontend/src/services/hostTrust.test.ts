import {afterEach, describe, expect, it} from 'vitest'
import {nextTick} from 'vue'
import {requestHostTrust} from './hostTrust'
const challenge = {id: 'request', host: '192.0.2.1:22', fingerprint: 'SHA256:new', keyType: 'ssh-ed25519', changed: true, oldFingerprints: ['SHA256:old'], canReplace: true}
const tick = async () => {await nextTick(); await nextTick()}
afterEach(() => {document.body.innerHTML = ''})
describe('host key change confirmation', () => {
 it('requires explicit fingerprint verification before replacement', async () => {
  const pending = requestHostTrust(challenge); await tick()
  const button = Array.from(document.querySelectorAll('button')).find(b => b.textContent?.includes('覆盖当前密钥'))!
  expect(button.disabled).toBe(true)
  const checkbox = document.querySelector('input[type=checkbox]') as HTMLInputElement
  checkbox.click(); await tick(); expect(button.disabled).toBe(false)
  button.click(); await expect(pending).resolves.toBe(true); await tick()
 })
 it('cannot replace a read-only trust source even when checked', async () => {
  const pending = requestHostTrust({...challenge, canReplace: false}); await tick()
  ;(document.querySelector('input[type=checkbox]') as HTMLInputElement).click(); await tick()
  const buttons = Array.from(document.querySelectorAll('button'))
  expect(buttons.find(b => b.textContent?.includes('覆盖当前密钥'))!.disabled).toBe(true)
  buttons.find(b => b.textContent === '取消连接')!.click()
  await expect(pending).resolves.toBe(false); await tick()
 })
})
