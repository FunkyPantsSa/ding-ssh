import {t} from '../i18n'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import {useToast} from '../composables/useToast'

export async function copyText(text: string): Promise<void> {
  try {
    const result = await ClipboardSetText(text)
    if (result === false) throw new Error(t("剪贴板写入失败"))
    useToast().success(t("复制成功"))
  } catch (error) {
    useToast().error(t("复制失败"), t("请重试"))
    throw error
  }
}
