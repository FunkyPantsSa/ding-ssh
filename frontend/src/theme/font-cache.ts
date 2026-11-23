/** IndexedDB also works in native WebViews that do not expose Cache Storage. */
export async function fontDataCache(url: string, data?: ArrayBuffer | null): Promise<ArrayBuffer | undefined> {
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open('ding-remote-fonts', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('fonts')
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
    request.onblocked = () => reject(new Error('Font cache is unavailable'))
  })
  try {
    return await new Promise<ArrayBuffer | undefined>((resolve, reject) => {
      const transaction = db.transaction('fonts', data === undefined ? 'readonly' : 'readwrite')
      const store = transaction.objectStore('fonts')
      const request = data === undefined ? store.get(url) : data === null ? store.delete(url) : store.put(data, url)
      let result: ArrayBuffer | undefined
      request.onsuccess = () => { if (data === undefined) result = request.result }
      transaction.oncomplete = () => resolve(result)
      transaction.onerror = () => reject(transaction.error)
      transaction.onabort = () => reject(transaction.error)
    })
  } finally { db.close() }
}
