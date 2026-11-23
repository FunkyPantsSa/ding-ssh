import {ref} from 'vue'
import {errorText} from '../i18n'
import {sshService} from '../services/ssh'
import type {SudoPromptEvent} from '../types'

interface Options {
  sessionId: () => string
  active: () => boolean
  focusTerminal: () => void
  resetInput: () => void
}

/** One instance per terminal; no stored password is ever loaded into this state. */
export function useSudoAssistant(options: Options) {
  const prompt = ref<SudoPromptEvent | null>(null)
  const sensitive = ref(false)
  const automatic = ref(false)
  const password = ref('')
  const submitting = ref(false)
  const error = ref('')
  let autoTimer: ReturnType<typeof setTimeout> | undefined
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let sequence = 0

  function stopTimers() {
    clearTimeout(autoTimer); clearTimeout(expiryTimer)
    autoTimer = undefined; expiryTimer = undefined
  }

  function hide() {
    stopTimers()
    prompt.value = null
    password.value = ''
    submitting.value = false
    automatic.value = false
    error.value = ''
  }

  function cancel(focus = true) {
    const current = prompt.value
    hide()
    if (current) void sshService.cancelSudoPrompt(options.sessionId(), current.requestId).catch(() => {})
    if (focus) options.focusTerminal()
  }

  function manual(initial = '') {
    clearTimeout(autoTimer)
    autoTimer = undefined
    automatic.value = false
    password.value = initial
  }

  async function submit() {
    const current = prompt.value
    if (!current || submitting.value) return
    if (!options.active() || Date.now() >= current.expiresAt) { cancel(false); return }
    const useSaved = automatic.value
    if (!useSaved && !password.value) return
    clearTimeout(autoTimer)
    autoTimer = undefined
    submitting.value = true
    error.value = ''
    const entered = password.value
    password.value = ''
    try {
      await sshService.submitSudoPassword(options.sessionId(), current.requestId, useSaved ? '' : entered, useSaved)
      // A retry prompt can arrive before the RPC finishes. Never close that newer prompt.
      if (prompt.value?.requestId === current.requestId) {
        hide()
        sensitive.value = false
        options.resetInput()
        options.focusTerminal()
      }
    } catch (cause) {
      if (prompt.value?.requestId === current.requestId) {
        error.value = errorText(cause)
        automatic.value = false
        submitting.value = false
      }
    }
  }

  function handleEvent(event: SudoPromptEvent) {
    if (event.sequence <= sequence) return
    sequence = event.sequence
    sensitive.value = event.sensitive
    options.resetInput()
    if (event.state !== 'prompt') {
      const hadPrompt = !!prompt.value
      hide()
      if (hadPrompt && options.active()) options.focusTerminal()
      return
    }
    if (prompt.value?.requestId === event.requestId) return
    hide()
    if (!options.active() || Date.now() >= event.expiresAt) {
      void sshService.cancelSudoPrompt(options.sessionId(), event.requestId).catch(() => {})
      return
    }
    prompt.value = event
    automatic.value = event.hasSavedPassword && !event.retry
    if (automatic.value) autoTimer = setTimeout(() => { void submit() }, 1000)
    expiryTimer = setTimeout(() => cancel(), Math.max(0, event.expiresAt - Date.now()))
  }

  // A keystroke/paste in xterm cancels any countdown synchronously. The backend
  // independently consumes the challenge before it forwards the same input.
  function terminalInput() {
    if (prompt.value) hide()
  }

  function reset() {
    hide()
    sensitive.value = false
    sequence = 0
  }

  return {prompt, sensitive, automatic, password, submitting, error, handleEvent, submit, manual, cancel, terminalInput, reset}
}
