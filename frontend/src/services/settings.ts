// 封装设置相关的 Wails 绑定。
import {GetSettings, SaveSettings} from '../../bindings/ding-ssh/internal/apphost/app'
import {Settings as SettingsModel} from '../../bindings/ding-ssh/internal/models/models'
import type {Settings} from '../types'

export const settingsService = {
  // models.Settings 的字面量联合比 src/types 宽松，结构一致，按需断言
  getSettings: (): Promise<Settings> => GetSettings() as Promise<Settings>,
  // 生成的 models.Settings 含嵌套对象转换逻辑，用 createFrom 构造后再提交
  saveSettings: (settings: Settings): Promise<void> =>
    SaveSettings(SettingsModel.createFrom(settings)),
}
