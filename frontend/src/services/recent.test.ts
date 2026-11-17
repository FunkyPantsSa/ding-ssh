import {afterEach, expect, it, vi} from 'vitest'

afterEach(() => {
  localStorage.clear()
  vi.resetModules()
})

it('migrates both legacy formats and counts successful logins separately from automatic reconnects', async () => {
  localStorage.setItem('ding-ssh:recent-servers', JSON.stringify(['legacy', {id: 'other', connectedAt: 20}]))
  const {recordRecentServer, serverUsage} = await import('./recent')

  recordRecentServer({id: 'legacy', name: 'Legacy', host: 'legacy', connectedAt: 30})
  recordRecentServer({id: 'other', name: 'Other', host: 'other', connectedAt: 40}, false)

  const expected = [
    {id: 'other', count: 1, connectedAt: 40},
    {id: 'legacy', count: 2, connectedAt: 30},
  ]
  expect(serverUsage.value).toEqual(expected)

  vi.resetModules()
  expect((await import('./recent')).serverUsage.value).toEqual(expected)
})
