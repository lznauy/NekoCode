import { describe, expect, it } from 'vitest'
import {
  isWailsEnvironment,
  safeAbort,
  safeEventsOn,
  safeCurrentModel,
	safeCommandMenu,
  safeSendMessage,
  safeReplyConfirm,
} from '../wails'

describe('isWailsEnvironment', () => {
  it('returns false when window.go is undefined', () => {
    expect(isWailsEnvironment()).toBe(false)
  })
})

describe('safe wrappers', () => {
  it('return no-ops when Wails is unavailable', async () => {
    expect(safeEventsOn('test', () => {})).toBeInstanceOf(Function)
    await expect(safeAbort()).resolves.toBeUndefined()
    await expect(safeCurrentModel()).resolves.toBe('')
    await expect(safeSendMessage('hello')).resolves.toBeUndefined()
    await expect(safeReplyConfirm('unavailable', true)).rejects.toBeDefined()
	await expect(safeCommandMenu('/')).resolves.toBeNull()
  })
})

it('provides the full generated usage shape outside Wails', async () => {
 const { safeBackgroundUsage } = await import('../wails')
 const usage = await safeBackgroundUsage('session')
 expect(usage).toEqual({total_tokens: 0, cache_hit_tokens: 0, cache_miss_tokens: 0, tool_use_count: 0, duration_ms: 0})
})
