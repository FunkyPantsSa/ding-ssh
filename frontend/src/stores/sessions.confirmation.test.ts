import {beforeEach, expect, it, vi} from 'vitest'
import {flushPromises} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {useSessionsStore} from './sessions'
import {useSettingsStore} from './settings'
import {confirmTabs} from '../services/closeConfirmation'
vi.mock('../services/closeConfirmation', () => ({confirmTabs: vi.fn(), needsTabConfirmation: () => true}))
beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })
it('keeps tabs and active state intact when closing is canceled', async () => {
 const store = useSessionsStore()
 store.tabs = [{clientId:'one'}, {clientId:'two'}] as any
 store.activeId = 'one'
 vi.mocked(confirmTabs).mockResolvedValue(false)
 store.closeTab('one')
 await flushPromises()
 expect(store.tabs).toHaveLength(2)
 expect(store.activeId).toBe('one')
})
it('confirms a batch only once and closes only its original targets', async () => {
 const store = useSessionsStore()
 useSettingsStore().loaded = true
 store.tabs = [{clientId:'one'}, {clientId:'two'}, {clientId:'three'}] as any
 vi.mocked(confirmTabs).mockResolvedValue(true)
 store.closeTabs(['one','two'])
 await flushPromises()
 expect(confirmTabs).toHaveBeenCalledExactlyOnceWith(2)
 expect(store.tabs.map(tab => tab.clientId)).toEqual(['three'])
})
