/** Text from xterm is Unicode; base64 debug input is already a binary string. */
export function encodeTerminalInput(data: string, binary = false): string {
  if (binary) return btoa(data)
  const bytes = new TextEncoder().encode(data)
  let raw = ''
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    raw += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000))
  }
  return btoa(raw)
}
