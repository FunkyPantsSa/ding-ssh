<script lang="ts" setup>
import {ref} from 'vue'
import ImportExportSection from './ImportExportSection.vue'
import {securityService} from '../../services/security'
import {useCredentialsStore} from '../../stores/credentials'
import {useServersStore} from '../../stores/servers'

const servers = useServersStore()
const credentials = useCredentialsStore()

const packOverwrite = ref(false)
const exportPass = ref('')
const exportPassConfirm = ref('')
const exportError = ref('')
const exportMsg = ref('')
const exportBusy = ref(false)
const importPass = ref('')
const importError = ref('')
const importMsg = ref('')
const importBusy = ref(false)

async function exportPack() {
  exportError.value = ''
  exportMsg.value = ''
  if (!exportPass.value || exportPass.value !== exportPassConfirm.value) {
    exportError.value = '请填写并确认导出密码'
    return
  }
  exportBusy.value = true
  try {
    const path = await securityService.exportConfig(exportPass.value)
    if (path) {
      exportMsg.value = `已导出到 ${path}`
      exportPass.value = ''
      exportPassConfirm.value = ''
    }
  } catch (e) {
    exportError.value = String(e)
  } finally {
    exportBusy.value = false
  }
}

async function importPack() {
  importError.value = ''
  importMsg.value = ''
  if (!importPass.value) {
    importError.value = '请填写导入密码'
    return
  }
  importBusy.value = true
  try {
    const r = await securityService.importConfig(importPass.value, packOverwrite.value)
    importMsg.value = `已导入服务器 ${r.servers}、凭证 ${r.credentials}、分组 ${r.groups}`
    importPass.value = ''
    await servers.load()
    await credentials.load()
  } catch (e) {
    importError.value = String(e)
  } finally {
    importBusy.value = false
  }
}
</script>

<template>
  <ImportExportSection
    v-model:export-pass="exportPass"
    v-model:export-pass-confirm="exportPassConfirm"
    v-model:import-pass="importPass"
    v-model:overwrite="packOverwrite"
    :export-error="exportError"
    :export-msg="exportMsg"
    :export-busy="exportBusy"
    :import-error="importError"
    :import-msg="importMsg"
    :import-busy="importBusy"
    @export="exportPack"
    @import="importPack"
  />
</template>
