import {flushPromises, mount} from '@vue/test-utils'
import {beforeEach, expect, it, vi} from 'vitest'
import SettingsSecuritySection from './SettingsSecuritySection.vue'
const mocks = vi.hoisted(() => ({status:vi.fn(), enable:vi.fn()}))
vi.mock('../../services/security',()=>({securityService:{getStatus:mocks.status,enableMasterPassword:mocks.enable}}))
vi.mock('./HostTrustSection.vue',()=>({default:{template:'<div />'}}))
beforeEach(()=>vi.resetAllMocks())
it('shows loading instead of inventing locked and keyring states',async()=>{
 let resolve!:(v:unknown)=>void
 mocks.status.mockReturnValue(new Promise(r=>{resolve=r}))
 const view=mount(SettingsSecuritySection)
 expect(view.text()).toContain('正在读取安全状态')
 expect(view.text()).not.toContain('未解锁')
 expect(view.text()).not.toContain('已回退本地密钥文件')
 expect(view.find('input').exists()).toBe(false)
 resolve({unlocked:true,masterPasswordEnabled:false,keyringAvailable:true,needsUnlock:false})
 await flushPromises()
 expect(view.text()).toContain('保险库已解锁')
 expect(view.text()).toContain('主密码：未启用')
 expect(view.find('input').exists()).toBe(true)
 view.unmount()
})
it('reports a failed status request and permits retry without showing password forms',async()=>{
 mocks.status.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce({unlocked:false,masterPasswordEnabled:true,keyringAvailable:false,needsUnlock:true})
 const view=mount(SettingsSecuritySection);await flushPromises()
 expect(view.text()).toContain('无法读取安全状态')
 expect(view.find('input').exists()).toBe(false)
 await view.get('button').trigger('click');await flushPromises()
 expect(view.text()).toContain('等待主密码解锁')
 expect(mocks.status).toHaveBeenCalledTimes(2)
 view.unmount()
})
it('does not submit duplicate master password changes',async()=>{
 mocks.status.mockResolvedValue({unlocked:true,masterPasswordEnabled:false,keyringAvailable:true,needsUnlock:false})
 let finish!:()=>void
 mocks.enable.mockReturnValue(new Promise<void>(r=>{finish=r}))
 const view=mount(SettingsSecuritySection);await flushPromises()
 await view.get('#settings-new-master').setValue('test-password')
 await view.get('#settings-confirm-master').setValue('test-password')
 const button=view.findAll('button').find(b=>b.text()==='启用')!
 await button.trigger('click');await button.trigger('click')
 expect(mocks.enable).toHaveBeenCalledTimes(1)
 finish();await flushPromises();view.unmount()
})
