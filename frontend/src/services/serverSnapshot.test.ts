import {beforeEach, expect, it, vi} from 'vitest'
import type {ServerNode, SysInfoSnapshot} from '../types'

const node = {id: 'audit', user: 'test', host: 'localhost', port: 22} as ServerNode
const snapshot = {hostname: 'fixture'} as unknown as SysInfoSnapshot

beforeEach(() => { vi.resetModules(); localStorage.clear() })

it.each(['null', '[]', '1', '"bad"', '{broken'])('recovers from invalid snapshot storage: %s', async raw => {
  localStorage.setItem('server-config-snapshots', raw)
  const service = await import('./serverSnapshot')
  expect(() => service.getServerSnapshot(node)).not.toThrow()
  expect(() => service.cacheServerSnapshot(node, snapshot)).not.toThrow()
  expect(service.getServerSnapshot(node)).toEqual(snapshot)
  expect(JSON.parse(localStorage.getItem('server-config-snapshots')!)).toEqual({'audit:test@localhost:22': snapshot})
  service.clearServerSnapshot(node.id)
  expect(service.getServerSnapshot(node)).toBeUndefined()
})

it('replaces obsolete host snapshots and preserves the last good sample on collection failure', async () => {
  const service = await import('./serverSnapshot')
  service.cacheServerSnapshot(node, snapshot)
  service.cacheServerSnapshot(node, {error: 'temporarily unavailable'} as SysInfoSnapshot)
  expect(service.getServerSnapshot(node)).toEqual({...snapshot, error: 'temporarily unavailable'})
  const moved = {...node, host: '127.0.0.1'}
  service.cacheServerSnapshot(moved, snapshot)
  expect(service.getServerSnapshot(node)).toBeUndefined()
  expect(service.getServerSnapshot(moved)).toEqual(snapshot)
})
