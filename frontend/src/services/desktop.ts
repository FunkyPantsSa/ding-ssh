import {Clipboard, Events, Window} from '@wailsio/runtime'
import {CopyText} from '../../bindings/ding-ssh/internal/apphost/app'

export {isDesktop} from './desktop-environment'

export function EventsOn<T>(name: string, callback: (data: T) => void): () => void {
  return Events.On(name, event => callback(event.data as T))
}

export const ClipboardGetText = Clipboard.Text
export const ClipboardSetText = (text: string) => CopyText(text)

export const WindowToggleMaximise = () => Window.ToggleMaximise()
export const WindowMinimise = () => Window.Minimise()
export const WindowClose = () => Window.Close()
export const WindowGetPosition = () => Window.Position()
export const WindowGetSize = async () => {
  const {width, height} = await Window.Size()
  return {w: width, h: height}
}
export const WindowIsMaximised = () => Window.IsMaximised()
export const WindowIsMinimised = () => Window.IsMinimised()
export const WindowMaximise = () => Window.Maximise()
export const WindowReload = () => Window.Reload()
export const WindowSetPosition = (x: number, y: number) => Window.SetPosition(x, y)
export const WindowSetSize = (width: number, height: number) => Window.SetSize(width, height)
export const WindowUnmaximise = () => Window.UnMaximise()
export const WindowUnminimise = () => Window.UnMinimise()
