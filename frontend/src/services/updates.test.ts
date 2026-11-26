import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {language} from '../i18n'
afterEach(() => { language.value = 'zh-CN' })
vi.mock('../../bindings/ding-ssh/internal/apphost/app', () => ({GetUpdateChannel: vi.fn(), CheckUpdate: vi.fn(), DownloadUpdate: vi.fn(), InstallUpdateNow: vi.fn()}))
vi.mock('./desktop', () => ({EventsOn: vi.fn(() => () => {})}))
import {CheckUpdate} from '../../bindings/ding-ssh/internal/apphost/app'
import {checkUpdates, checkUpdatesOnStartup, updateInfo, updateBusy, updateReady, updateVisible, updateMessage, dismissUpdate, setUpdateChannel, updateChannel} from './updates'
beforeEach(() => {vi.clearAllMocks(); updateBusy.value=false; updateReady.value=false; updateInfo.value=undefined; updateVisible.value=false})
it('enables startup checks by default', () => {expect(checkUpdatesOnStartup.value).toBe(true)})
it('keeps up-to-date manual results in About', async () => {
 vi.mocked(CheckUpdate).mockResolvedValue({available:false,current:'abc',manifest:{commit:'abc',channel:'dev',builtAt:'',assets:{}}} as any)
 await checkUpdates(true);expect(updateVisible.value).toBe(false)
})
it('suppresses dismissed automatic reminders while allowing manual checks', async () => {
 vi.mocked(CheckUpdate).mockResolvedValue({available:true,current:'old',manifest:{commit:'new',channel:'dev',builtAt:'',assets:{}}} as any)
 await checkUpdates();expect(updateVisible.value).toBe(true)
 dismissUpdate();await checkUpdates();expect(updateVisible.value).toBe(false)
 await checkUpdates(true);expect(updateVisible.value).toBe(true)
})
it('persists channel selection and clears stale prompts', () => {
 updateVisible.value=true;setUpdateChannel('dev')
 expect(updateChannel.value).toBe('dev');expect(localStorage.getItem('update-channel')).toBe('dev');expect(updateVisible.value).toBe(false)
})

it('translates existing update status when the interface language changes', async () => {
 vi.mocked(CheckUpdate).mockResolvedValue({available:false,current:'abc',manifest:{commit:'abc',channel:'dev',builtAt:'',assets:{}}} as any)
 language.value = 'zh-CN'
 await checkUpdates(true)
 expect(updateMessage.value).toBe('当前已是最新版本')
 language.value = 'en'
 expect(updateMessage.value).toBe('You are up to date')
 language.value = 'zh-CN'
 expect(updateMessage.value).toBe('当前已是最新版本')
})
