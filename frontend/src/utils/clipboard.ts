import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import {useToast} from '../composables/useToast'

export async function copyText(text: string): Promise<void> {
  try {
    const result = await ClipboardSetText(text)
    if (result === false) throw new Error('剪贴板写入失败')
    useToast().success('复制成功')
  } catch (error) {
    useToast().error('复制失败', '请重试')
    throw error
  }
}
