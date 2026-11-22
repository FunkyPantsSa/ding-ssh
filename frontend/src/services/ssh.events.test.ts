import {beforeEach, expect, it, vi} from 'vitest'
import {EventsOn} from './desktop'
import {onSftpTransfer} from './ssh'

vi.mock('./desktop', () => ({EventsOn: vi.fn(() => () => {})}))

beforeEach(() => vi.clearAllMocks())

it.each(['transferId', 'TransferID'])('preserves %s through the real event adapter for same-name cancellation', key => {
  const receive = vi.fn()
  onSftpTransfer('session', receive)
  const emit = vi.mocked(EventsOn).mock.calls[0]![1]
  for (const id of ['first', 'second']) {
    emit({[key]: id, sessionId: 'session', direction: 'upload', name: 'same.txt', transferred: 2, total: 4, done: false})
  }
  expect(receive.mock.calls.map(([event]) => event.transferId)).toEqual(['first', 'second'])
})

it('retains compatibility with legacy transfer events without an ID', () => {
  const receive = vi.fn()
  onSftpTransfer('session', receive)
  const emit = vi.mocked(EventsOn).mock.calls[0]![1]
  emit(['session', 'download', 'old.txt', 2, 4, false])
  expect(receive).toHaveBeenCalledWith(expect.objectContaining({name: 'old.txt', transferred: 2}))
})
