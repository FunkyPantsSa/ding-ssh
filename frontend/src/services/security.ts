import {
  ChangeMasterPassword,
  DisableMasterPassword,
  EnableMasterPassword,
  ExportConfig,
  GetSecurityStatus,
  ImportConfig,
  UnlockWithMasterPassword,
} from '../../wailsjs/go/main/App'
import type {ImportConfigResult, SecurityStatus} from '../types'
import {errorText, t} from '../i18n'

/** Startup must remain locked when the security boundary cannot be checked. */
export async function getStartupSecurityStatus(): Promise<SecurityStatus> {
  try {
    const status = await securityService.getStatus()
    if (typeof status?.unlocked !== 'boolean' || typeof status?.needsUnlock !== 'boolean') {
      throw new Error(t('保险库未初始化'))
    }
    return status
  } catch (error) {
    return {unlocked: false, needsUnlock: true, masterPasswordEnabled: false, keyringAvailable: false,
      initializationError: errorText(error) || t('保险库未初始化')}
  }
}

export const securityService = {
  getStatus: (): Promise<SecurityStatus> => GetSecurityStatus(),
  unlock: (password: string): Promise<void> => UnlockWithMasterPassword(password),
  enableMasterPassword: (password: string): Promise<void> => EnableMasterPassword(password),
  disableMasterPassword: (password: string): Promise<void> => DisableMasterPassword(password),
  changeMasterPassword: (oldPassword: string, newPassword: string): Promise<void> =>
    ChangeMasterPassword(oldPassword, newPassword),
  exportConfig: (passphrase: string): Promise<string> => ExportConfig(passphrase),
  importConfig: (passphrase: string, overwrite: boolean): Promise<ImportConfigResult> =>
    ImportConfig(passphrase, overwrite),
}
