import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it} from 'vitest'
import {buildCommands} from './commands'
import {useServersStore} from './stores/servers'
import {useSessionsStore} from './stores/sessions'
import {useUIStore} from './stores/ui'

beforeEach(() => setActivePinia(createPinia()))

describe('command navigation IA', () => {
  it('keeps the original top-level views and does not add overview/transfers routes', () => {
    const commands = buildCommands({sessions: useSessionsStore(), servers: useServersStore(), ui: useUIStore()})
    const ids = commands.map((command) => command.id)

    expect(ids).toContain('workspace')
    expect(ids).toContain('servers')
    expect(ids).toContain('tunnel')
    expect(ids).toContain('settings')
    expect(ids).not.toContain('overview')
    expect(ids).not.toContain('transfers')
  })
})
