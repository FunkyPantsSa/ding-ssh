import {expect, it, vi} from 'vitest'
import {guardProgrammaticSelection} from './selection-copy-guard'
it('suppresses search selections including later refreshes, but preserves mouse selection copying', async () => {
 const copy = vi.fn()
 const onSelectionChange = () => { if (!guard.isProgrammatic()) copy() }
 const terminal = {select: vi.fn((_column: number, _row: number, _length: number) => onSelectionChange())}
 const original = terminal.select
 const guard = guardProgrammaticSelection(terminal)
 terminal.select(0, 10, 3)
 await Promise.resolve().then(() => terminal.select(0, 20, 3))
 await Promise.resolve().then(onSelectionChange)
 expect(copy).not.toHaveBeenCalled()
 guard.beginUserSelection()
 onSelectionChange()
 expect(copy).toHaveBeenCalledOnce()
 guard.dispose()
 expect(terminal.select).toBe(original)
})
it('keeps failed programmatic selections suppressed until a user gesture', () => {
 const terminal = {select: () => { throw new Error('disposed') }}
 const guard = guardProgrammaticSelection(terminal)
 expect(() => terminal.select()).toThrow('disposed')
 expect(guard.isProgrammatic()).toBe(true)
 guard.beginUserSelection()
 expect(guard.isProgrammatic()).toBe(false)
})
