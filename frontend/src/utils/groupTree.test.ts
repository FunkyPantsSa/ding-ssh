import {afterEach, describe, expect, it} from 'vitest'
import {language} from '../i18n'

afterEach(() => { language.value = 'zh-CN' })
import {
  ancestorsToExpand,
  buildGroupTree,
  collectGroupPaths,
  filterGroupTree,
  flattenGroupTree,
  groupAncestors,
  groupDepth,
  groupName,
  groupParent,
  isGroupInSubtree,
  normalizeGroupPath,
  relativeGroupPath,
  retargetGroupPath,
  validateGroupNameInput,
} from './groupTree'

describe('group path helpers', () => {
  it('localizes validation limits while preserving group names', () => {
    language.value = 'en'
    expect(validateGroupNameInput('')).toBe('Enter a group name')
    expect(validateGroupNameInput('生产/华东/杭州', 2)).toBe('Groups cannot exceed 2 levels')
    expect(validateGroupNameInput('服务器', 6, 2)).toBe('Each group name must be at most 2 characters')
    expect(validateGroupNameInput('生产/华东')).toBe('')
    language.value = 'zh-CN'
    expect(validateGroupNameInput('a/b/c', 2)).toBe('分组层级不能超过 2 级')
  })

  it('normalizes paths and derives name/parent/depth', () => {
    expect(normalizeGroupPath(' /生产环境//华东/ ')).toBe('生产环境/华东')
    expect(groupName('生产环境/华东/杭州')).toBe('杭州')
    expect(groupParent('生产环境/华东/杭州')).toBe('生产环境/华东')
    expect(groupParent('生产环境')).toBe('')
    expect(groupDepth('生产环境/华东/杭州')).toBe(3)
    expect(groupDepth('')).toBe(0)
    expect(groupAncestors('生产环境/华东/杭州')).toEqual(['生产环境', '生产环境/华东'])
  })

  it('matches subtrees only on path segments', () => {
    expect(isGroupInSubtree('生产环境/华东', '生产环境')).toBe(true)
    expect(isGroupInSubtree('生产环境/华东', '生产环境/华东')).toBe(true)
    expect(isGroupInSubtree('生产环境2', '生产环境')).toBe(false)
    expect(isGroupInSubtree('', '')).toBe(true)
    expect(isGroupInSubtree('生产环境', '')).toBe(false)
  })

  it('renders relative paths inside a group view', () => {
    expect(relativeGroupPath('生产环境/华东/杭州', '生产环境/华东')).toBe('杭州')
    expect(relativeGroupPath('生产环境/华东', '生产环境/华东')).toBe('')
    expect(relativeGroupPath('测试环境/预发', '生产环境')).toBe('测试环境/预发')
  })

  it('retargets a path after renaming an ancestor', () => {
    expect(retargetGroupPath('生产环境/华东', '生产环境', 'Prod')).toBe('Prod/华东')
    expect(retargetGroupPath('生产环境', '生产环境', 'Prod')).toBe('Prod')
    expect(retargetGroupPath('测试环境', '生产环境', 'Prod')).toBe('测试环境')
  })

  it('validates input with depth, empty segment and control chars', () => {
    expect(validateGroupNameInput('')).toBe('请输入分组名称')
    expect(validateGroupNameInput('/生产')).toBe('分组名称不能以 / 开头或结尾')
    expect(validateGroupNameInput('生产//华东')).toBe('分组路径不能包含空层级')
    expect(validateGroupNameInput('a/b/c/d/e/f/g')).toContain('不能超过 6 级')
    expect(validateGroupNameInput('a\u0007b')).toBe('分组名称不能包含控制字符')
    expect(validateGroupNameInput('生产环境/华东')).toBe('')
  })
})

describe('buildGroupTree', () => {
  it('adds implicit ancestors and counts direct and total servers', () => {
    const tree = buildGroupTree(
      ['测试环境'],
      [
        {group: '生产环境/华东/杭州'},
        {group: '生产环境/华东/杭州'},
        {group: '生产环境/华东/上海'},
        {group: '生产环境/华南'},
        {group: ''},
      ],
    )

    expect(tree.map((node) => node.name)).toEqual(['测试环境', '生产环境'])
    const prod = tree.find((node) => node.path === '生产环境')!
    expect(prod.direct).toBe(0)
    expect(prod.total).toBe(4)
    const east = prod.children.find((node) => node.path === '生产环境/华东')!
    expect(east.children.map((node) => node.name)).toEqual(['杭州', '上海'])
    expect(east.direct).toBe(0)
    expect(east.total).toBe(3)

    const hangzhou = east.children.find((node) => node.path === '生产环境/华东/杭州')!
    expect(hangzhou.direct).toBe(2)
    expect(hangzhou.total).toBe(2)
    expect(hangzhou.depth).toBe(3)
  })

  it('keeps empty explicit groups and ignores ungrouped servers', () => {
    const tree = buildGroupTree(['空分组'], [{group: ''}, {group: '  '}])
    expect(tree).toHaveLength(1)
    expect(tree[0]).toMatchObject({path: '空分组', direct: 0, total: 0})
  })

  it('collects paths and flattens according to expansion', () => {
    const tree = buildGroupTree(['a/b', 'a/c', 'd'], [])
    expect(collectGroupPaths(tree)).toEqual(['a', 'a/b', 'a/c', 'd'])
    expect(flattenGroupTree(tree, new Set(['a'])).map((node) => node.path)).toEqual([
      'a',
      'a/b',
      'a/c',
      'd',
    ])
    expect(flattenGroupTree(tree, new Set()).map((node) => node.path)).toEqual(['a', 'd'])
  })
})

describe('filterGroupTree', () => {
  const tree = buildGroupTree(['生产环境/华东/杭州', '生产环境/华南', '测试环境'], [])

  it('keeps matched nodes with their full subtree', () => {
    const filtered = filterGroupTree(tree, '生产')
    expect(filtered).toHaveLength(1)
    expect(filtered[0].path).toBe('生产环境')
    expect(filtered[0].children.map((node) => node.path)).toEqual([
      '生产环境/华东',
      '生产环境/华南',
    ])
  })

  it('keeps ancestor chain when only a deep child matches', () => {
    const filtered = filterGroupTree(tree, '杭州')
    expect(filtered).toHaveLength(1)
    expect(filtered[0].path).toBe('生产环境')
    expect(filtered[0].children).toHaveLength(1)
    expect(filtered[0].children[0].path).toBe('生产环境/华东')
    expect(filtered[0].children[0].children[0].path).toBe('生产环境/华东/杭州')
  })

  it('returns original tree for empty keyword', () => {
    expect(filterGroupTree(tree, '  ')).toBe(tree)
  })

  it('returns empty list when nothing matches', () => {
    expect(filterGroupTree(tree, '不存在')).toEqual([])
  })
})

describe('expanded ancestors', () => {
  it('returns ancestors for auto expansion', () => {
    expect(ancestorsToExpand('生产环境/华东/杭州')).toEqual(['生产环境', '生产环境/华东'])
  })
})
