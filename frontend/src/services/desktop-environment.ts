export function isDesktop(): boolean {
  if (typeof window === 'undefined') return false
  const host = window as Window & {_wails?: {environment?: {OS?: string}}}
  const platform = host._wails?.environment?.OS
  return platform === 'darwin' || platform === 'windows' || platform === 'linux'
}
