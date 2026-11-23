// 封装凭证相关的 Wails 绑定。
import {DeleteCredential, GetCredentials, SaveCredential} from '../../bindings/ding-ssh/internal/apphost/app'
import type {Credential} from '../types'

export const credentialService = {
  list: (): Promise<Credential[]> => GetCredentials(),
  save: (c: Credential): Promise<Credential> => SaveCredential(c),
  remove: (id: string): Promise<void> => DeleteCredential(id),
}
