import {afterEach, expect, it} from 'vitest'
import {mount} from '@vue/test-utils'
import {nextTick} from 'vue'
import {language} from '../../i18n'
import {buildGroupTree} from '../../utils/groupTree'
import GroupTree from './GroupTree.vue'

afterEach(() => { language.value = 'zh-CN' })

it('switches group labels and tooltips while preserving user group names', async () => {
  const wrapper = mount(GroupTree, {props: {
    nodes: buildGroupTree(['生产环境/华东'], [{group: '生产环境/华东'}]),
    activePath: '', expanded: new Set<string>(), dragOver: null,
  }})
  try {
    const row = wrapper.get('[data-group-path="生产环境"]')
    expect(row.attributes('aria-label')).toBe('生产环境，共 1 台服务器')
    language.value = 'en'
    await nextTick()
    expect(wrapper.get('[role="tree"]').attributes('aria-label')).toBe('Group tree')
    expect(row.attributes('aria-label')).toBe('生产环境, 1 servers in total')
    expect(row.attributes('title')).toBe('生产环境 · 0 direct servers · 1 including subgroups')
    expect(row.get('.group-tree-disclosure').attributes('aria-label')).toBe('Expand group')
    expect(row.text()).toContain('生产环境')
    language.value = 'zh-CN'
    await nextTick()
    expect(row.get('.group-tree-disclosure').attributes('aria-label')).toBe('展开分组')
  } finally { wrapper.unmount() }
})
