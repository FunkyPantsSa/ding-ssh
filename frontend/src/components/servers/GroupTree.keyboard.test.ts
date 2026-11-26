import {flushPromises, mount} from '@vue/test-utils'
import {defineComponent, ref} from 'vue'
import {afterEach, expect, it} from 'vitest'
import {buildGroupTree} from '../../utils/groupTree'
import GroupTree from './GroupTree.vue'

afterEach(() => { document.body.innerHTML = '' })

it('navigates visible tree rows and uses Enter to select without activating nested actions', async () => {
  const wrapper = mount(defineComponent({
    components: {GroupTree},
    setup() {
      const expanded = ref(new Set<string>())
      const active = ref('')
      const nodes = buildGroupTree(['Prod/East/App', 'Prod/West', 'Test'], [])
      return {expanded, active, nodes, toggle: (path: string) => {
        const next = new Set(expanded.value)
        if (next.has(path)) next.delete(path); else next.add(path)
        expanded.value = next
      }}
    },
    template: `<GroupTree :nodes="nodes" :active-path="active" :expanded="expanded" :drag-over="null" @toggle="toggle" @select="active = $event.path" />`,
  }), {attachTo: document.body})
  try {
    const tree = wrapper.getComponent(GroupTree)
    const row = (path: string) => wrapper.get<HTMLElement>(`[data-group-path="${path}"]`)
    const press = async (key: string, shiftKey = false) => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {key, shiftKey, bubbles: true, cancelable: true}))
      await flushPromises()
    }
    const focused = () => document.activeElement?.getAttribute('data-group-path')
    expect(wrapper.findAll('[role="treeitem"][tabindex="0"]')).toHaveLength(1)
    row('Prod').element.focus()
    await press('ArrowRight')
    expect(focused()).toBe('Prod')
    expect(row('Prod').attributes('aria-expanded')).toBe('true')
    await press('ArrowRight')
    expect(focused()).toBe('Prod/East')
    await press('ArrowRight')
    await press('ArrowDown')
    expect(focused()).toBe('Prod/East/App')
    await press('ArrowLeft')
    expect(focused()).toBe('Prod/East')
    await press('ArrowLeft')
    expect(row('Prod/East').attributes('aria-expanded')).toBe('false')
    await press('ArrowDown')
    expect(focused()).toBe('Prod/West')
    await press('Enter')
    expect(tree.emitted('select')?.at(-1)).toEqual([{kind: 'group', path: 'Prod/West'}])
    await press('End')
    expect(focused()).toBe('Test')
    row('Test').element.hidden = true
    await press('Home')
    expect(focused()).toBe('Prod')
    await press('End')
    expect(focused()).toBe('Prod/West')
    await press('F10', true)
    expect(tree.emitted('contextMenu')?.at(-1)?.[1]).toBe('Prod/West')
    const selectCount = tree.emitted('select')?.length
    row('Prod/West').get<HTMLButtonElement>('.group-tree-more').element.focus()
    await press('Enter')
    await press('Delete')
    expect(tree.emitted('select')).toHaveLength(selectCount!)
    expect(tree.emitted('remove')).toBeUndefined()
    expect(wrapper.findAll('[role="treeitem"][tabindex="0"]')).toHaveLength(1)
  } finally { wrapper.unmount() }
})
