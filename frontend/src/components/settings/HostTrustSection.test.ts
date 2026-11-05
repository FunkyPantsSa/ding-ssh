import {mount, flushPromises} from '@vue/test-utils'
import {expect, it, vi} from 'vitest'
import HostTrustSection from './HostTrustSection.vue'
it('limits the trust list to ten records and searches host addresses', async () => {
 ;(window as any).go = {main: {App: {GetHostTrust: vi.fn().mockResolvedValue(Array.from({length: 25}, (_, i) => ({line: i + 1, hosts: `192.0.2.${i + 1}`, fingerprint: `SHA256:${i}`, source: 'app', keyType: 'ssh-ed25519', readOnly: false})))}}}
 const view = mount(HostTrustSection); await flushPromises()
 expect(view.text()).toContain('共 25 条')
 expect(view.text()).not.toContain('192.0.2.11')
 await view.get('[aria-label="搜索已信任主机"]').setValue('192.0.2.25')
 expect(view.text()).toContain('共 1 条')
 expect(view.text()).toContain('192.0.2.25'); view.unmount()
})
