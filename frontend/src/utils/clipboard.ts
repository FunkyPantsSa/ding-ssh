import {t} from '../i18n'
import {ClipboardSetText} from '../services/desktop'
import {useToast} from '../composables/useToast'

export async function copyText(text: string): Promise<void> {
  try {
    await ClipboardSetText(text)
    useToast().success(t("复制成功"))
  } catch (error) {
    useToast().error(t("复制失败"), t("请重试"))
    throw error
  }
}
