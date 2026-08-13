// Trie 前缀树：用于静态字典与屏幕词库的前缀筛选。

export class TrieNode {
  children = new Map<string, TrieNode>()
  end = false
  value = ''
}

export class Trie {
  root = new TrieNode()

  insert(word: string) {
    if (!word) return
    let node = this.root
    for (const ch of word) {
      let next = node.children.get(ch)
      if (!next) {
        next = new TrieNode()
        node.children.set(ch, next)
      }
      node = next
    }
    node.end = true
    node.value = word
  }

  insertMany(words: Iterable<string>) {
    for (const w of words) this.insert(w)
  }

  /** 返回以 prefix 开头的词，最多 limit 条。 */
  prefixSearch(prefix: string, limit = 8): string[] {
    let node = this.root
    for (const ch of prefix) {
      const next = node.children.get(ch)
      if (!next) return []
      node = next
    }
    const out: string[] = []
    this.collect(node, out, limit)
    return out
  }

  private collect(node: TrieNode, out: string[], limit: number) {
    if (out.length >= limit) return
    if (node.end && node.value) {
      out.push(node.value)
      if (out.length >= limit) return
    }
    for (const child of node.children.values()) {
      this.collect(child, out, limit)
      if (out.length >= limit) return
    }
  }
}
