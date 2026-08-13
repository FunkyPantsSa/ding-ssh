// 轻量 FZF 风格模糊匹配：子序列匹配 + 连续加分。

export interface FuzzyHit {
  text: string
  score: number
}

/** 若 query 是 text 的子序列则返回分数，否则 -1。 */
export function fuzzyScore(text: string, query: string): number {
  if (!query) return 0
  if (!text) return -1
  const t = text.toLowerCase()
  const q = query.toLowerCase()
  let ti = 0
  let score = 0
  let streak = 0
  let first = -1
  for (let qi = 0; qi < q.length; qi++) {
    const ch = q[qi]
    let found = false
    while (ti < t.length) {
      if (t[ti] === ch) {
        if (first < 0) first = ti
        // 连续匹配加分；单词边界（开头或前一字符非字母数字）加分
        if (streak > 0) score += 8
        else if (ti === 0 || !/[a-z0-9]/i.test(t[ti - 1])) score += 6
        else score += 2
        streak++
        ti++
        found = true
        break
      }
      streak = 0
      ti++
    }
    if (!found) return -1
  }
  // 越靠前、越短越好
  score += Math.max(0, 40 - first)
  score += Math.max(0, 30 - (t.length - q.length))
  return score
}

export function fuzzyFilter(candidates: string[], query: string, limit = 8): FuzzyHit[] {
  if (!query) {
    return candidates.slice(0, limit).map((text) => ({text, score: 0}))
  }
  const hits: FuzzyHit[] = []
  for (const text of candidates) {
    const score = fuzzyScore(text, query)
    if (score >= 0) hits.push({text, score})
  }
  hits.sort((a, b) => b.score - a.score || a.text.length - b.text.length)
  return hits.slice(0, limit)
}
