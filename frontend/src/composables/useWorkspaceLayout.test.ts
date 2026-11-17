import {mount} from '@vue/test-utils'
import {defineComponent, nextTick, ref} from 'vue'
import {beforeEach, expect, it, vi} from 'vitest'
import {useWorkspaceLayout} from './useWorkspaceLayout'
vi.mock('../stores/settings', () => ({useSettingsStore: () => ({uiScale:125})}))
beforeEach(() => localStorage.clear())
function setup() {
 let layout!: ReturnType<typeof useWorkspaceLayout>
 const collapsed = ref(false)
 const wrapper = mount(defineComponent({setup(){layout=useWorkspaceLayout(collapsed,ref(null),ref(false));return () => null}}))
 return {layout,collapsed,wrapper}
}
it('restores widths, persists only after dragging and resets to defaults', async () => {
 localStorage.setItem('ding-ssh:nav-width','300')
 const {layout,wrapper}=setup()
 expect(layout.effectiveNavW.value).toBe(300)
 layout.onNavResize(350)
 expect(localStorage.getItem('ding-ssh:nav-width')).toBe('300')
 layout.onNavDrag(false)
 expect(localStorage.getItem('ding-ssh:nav-width')).toBe('350')
 layout.resetNavWidth()
 layout.onToolResize(500)
 layout.onToolDrag(false)
 expect(localStorage.getItem('ding-ssh:tool-width')).toBe('500')
 layout.resetToolWidth()
 await nextTick()
 expect(layout.effectiveNavW.value).toBe(216)
 expect(layout.effectiveToolW.value).toBe(340)
 wrapper.unmount()
})
it('rejects invalid stored widths and synchronizes collapsed navigation and zoom', async () => {
 localStorage.setItem('ding-ssh:nav-width','NaN')
 localStorage.setItem('ding-ssh:tool-width','-12')
 const {layout,collapsed,wrapper}=setup()
 expect(layout.effectiveNavW.value).toBe(216)
 expect(layout.effectiveToolW.value).toBe(340)
 collapsed.value=true
 await nextTick()
 expect(document.documentElement.style.getPropertyValue('--nav-w')).toBe('var(--nav-w-collapsed)')
 expect(document.documentElement.style.getPropertyValue('--ui-zoom')).toBe('1.25')
 wrapper.unmount()
})
it('shrinks panels in constrained windows and restores the preferred width when space returns', async () => {
 const {layout,wrapper}=setup()
 layout.shellEl.value = {$el:{clientWidth:650} as HTMLElement}
 layout.onNavResize(400)
 expect(layout.effectiveNavW.value).toBe(230)
 layout.shellEl.value = {$el:{clientWidth:1200} as HTMLElement}
 await nextTick()
 expect(layout.effectiveNavW.value).toBe(400)
 wrapper.unmount()
})
