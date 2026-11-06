import {expect, it} from 'vitest'
import {formatFontSize, validateFontHeader} from './custom-fonts'

it('rejects renamed non-font files and truncated files', () => {
  expect(() => validateFontHeader(new TextEncoder().encode('this is not a font file').buffer)).toThrow('不是字体文件')
  expect(() => validateFontHeader(new ArrayBuffer(4))).toThrow('不是字体文件')
})
it('recognizes supported font signatures', () => {
  for (const signature of [0x00010000, 0x4f54544f, 0x74727565, 0x774f4646, 0x774f4632]) {
    const data = new ArrayBuffer(12)
    new DataView(data).setUint32(0, signature)
    expect(() => validateFontHeader(data)).not.toThrow()
  }
})
it('formats persisted font data sizes including large fonts', () => {
  expect(formatFontSize(200 * 1024 ** 2)).toBe('200.0 MB')
  expect(formatFontSize(2048)).toBe('2.0 KB')
})
