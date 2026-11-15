import {afterEach, describe, expect, it, vi} from 'vitest'
import {errorText, language, syncBackendLanguage, t} from './i18n'
import en from './locales/en.json'

afterEach(() => { language.value = 'zh-CN'; delete (window as any).go })
describe('application translations', () => {
  it('defaults to Chinese and switches all translation calls reactively', () => {
    language.value = 'zh-CN'
    expect(t('设置')).toBe('设置')
    language.value = 'en'
    expect(t('设置')).toBe('Settings')
    expect(t('未登记的用户内容')).toBe('未登记的用户内容')
  })
  it('interpolates values without translating user content', () => {
    language.value = 'en'
    expect(t('连接 {0}', ['生产服务器'])).toBe('Connect to 生产服务器')
    expect(t('共 {0} 条 · {1}/{2} 页', [25, 1, 3])).toBe('25 records · Page 1/3')
    language.value = 'zh-CN'
    expect(t('共 {0} 条 · {1}/{2} 页', [25, 1, 3])).toBe('共 25 条 · 1/3 页')
  })
  it('keeps placeholder sets identical across the entire catalog', () => {
    for (const [source, translated] of Object.entries(en)) {
      expect(translated.trim(), source).not.toBe('')
      expect([...translated.matchAll(/\{\d+\}/g)].map(m => m[0]).sort(), source)
        .toEqual([...source.matchAll(/\{\d+\}/g)].map(m => m[0]).sort())
    }
  })
  it('localizes vault sentinel errors while preserving technical details', () => {
    language.value = 'en'
    expect(errorText(new Error('主密码错误'))).toBe('Error: Incorrect master password')
    expect(errorText('connection refused: 192.0.2.1')).toBe('connection refused: 192.0.2.1')
  })
  it('synchronizes native dialog language before the app starts', async () => {
    const SetUILanguage = vi.fn().mockResolvedValue(undefined)
    ;(window as any).go = {main: {App: {SetUILanguage}}}
    await syncBackendLanguage('en')
    expect(SetUILanguage).toHaveBeenCalledWith('en')
  })
})
