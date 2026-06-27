import { describe, expect, it } from 'vitest'
import { compactArgs, pathFromArgs } from './helpers'

describe('run helpers', () => {
  it('shows only the path for edit args formatted as key=value pairs', () => {
    const args = 'newString="next,value",oldString="old,value",path=/tmp/file.go,replaceAll=false'

    expect(compactArgs(args)).toBe('/tmp/file.go')
    expect(pathFromArgs(args)).toBe('/tmp/file.go')
  })

  it('keeps bash command previews readable for key=value pairs', () => {
    expect(compactArgs('command=echo hello')).toBe('echo hello')
  })
})
