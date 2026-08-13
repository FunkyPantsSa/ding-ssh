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
