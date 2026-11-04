import {describe, expect, it} from 'vitest'
import {fuzzyFilter, fuzzyScore} from './fzf'

describe('fuzzy matching', () => {
  it('rejects non-subsequences and rewards contiguous matches', () => {
    expect(fuzzyScore('terminal', 'tml')).toBeGreaterThan(0)
    expect(fuzzyScore('terminal', 'tx')).toBe(-1)
    expect(fuzzyScore('terminal', 'ter')).toBeGreaterThan(fuzzyScore('terminal', 'trl'))
  })

  it('sorts hits and respects the requested limit', () => {
    const hits = fuzzyFilter(['server-production', 'ssh-prod', 'archive'], 'pr', 1)
    expect(hits).toHaveLength(1)
    expect(hits[0].text).toBe('ssh-prod')
  })
})
