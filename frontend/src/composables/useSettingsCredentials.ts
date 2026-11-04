import { onBeforeUnmount, ref } from 'vue'
import { ClipboardSetText } from '../../wailsjs/runtime/runtime'
import { useCredentialsStore } from '../stores/credentials'
import type { Credential } from '../types'

/** Local interaction state for saved credentials, including short-lived reveal/copy state. */
export function useSettingsCredentials() {
  const credentials = useCredentialsStore()
  const showDialog = ref(false); const confirmId = ref(''); const editing = ref<Credential | null>(null)
  const revealId = ref(''); const revealKey = ref<Credential | null>(null); const copiedId = ref('')
  let copiedTimer: number | undefined
  const openNew = () => { editing.value = null; showDialog.value = true }
  const openEdit = (credential: Credential) => { editing.value = credential; showDialog.value = true }
  const toggleReveal = (credential: Credential) => {
    if (credential.authType === 'privateKey') { revealKey.value = credential; return }
    revealId.value = revealId.value === credential.id ? '' : credential.id
  }
  const copy = async (credential: Credential) => {
    const text = credential.authType === 'privateKey' ? credential.keyContent : credential.password
    if (!text) return
    try {
      await ClipboardSetText(text); copiedId.value = credential.id
      window.clearTimeout(copiedTimer)
      copiedTimer = window.setTimeout(() => { copiedId.value = '' }, 1600)
    } catch { /* clipboard unavailable */ }
  }
  const remove = async (id: string) => { confirmId.value = ''; await credentials.remove(id) }
  onBeforeUnmount(() => window.clearTimeout(copiedTimer))
  return { credentials, showDialog, confirmId, editing, revealId, revealKey, copiedId, openNew, openEdit, toggleReveal, copy, remove }
}
