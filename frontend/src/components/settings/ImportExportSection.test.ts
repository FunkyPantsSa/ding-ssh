import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import ImportExportSection from './ImportExportSection.vue'

const props = {exportPass: '', exportPassConfirm: '', exportError: '', exportMsg: '', exportBusy: false, importPass: '', importError: '', importMsg: '', importBusy: false, overwrite: false}

describe('settings import/export contract', () => {
  it('forwards password values and export action', async () => {
    const wrapper = mount(ImportExportSection, {props})
    await wrapper.get('[data-testid="settings-export-password"]').setValue('secret')
    await wrapper.get('[data-testid="settings-export"]').trigger('click')
    expect(wrapper.emitted('update:exportPass')?.[0]).toEqual(['secret'])
    expect(wrapper.emitted('export')).toHaveLength(1)
  })

  it('associates every password input with an explicit label', () => {
    const wrapper = mount(ImportExportSection, {props})
    for (const id of ['settings-export-password', 'settings-export-password-confirm', 'settings-import-password']) {
      expect(wrapper.find(`#${id}`).exists()).toBe(true)
      expect(wrapper.find(`label[for="${id}"]`).exists()).toBe(true)
    }
  })
})
