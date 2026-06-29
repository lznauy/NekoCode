import { createRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { InputBar } from '../InputBar'

function setup(overrides: Partial<Parameters<typeof InputBar>[0]> = {}) {
  const props = {
    text: '',
    busy: false,
    textareaRef: createRef<HTMLTextAreaElement>(),
    onChange: vi.fn(),
    onSend: vi.fn(),
    onStop: vi.fn(),
    onTextareaChange: vi.fn(),
    ...overrides,
  }
  const result = render(<InputBar {...props} />)
  return { ...result, props }
}

describe('InputBar', () => {
  it('renders textarea and send button', () => {
    setup()
    expect(screen.getByRole('textbox')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /发送/ })).toBeInTheDocument()
  })

  it('calls onSend when Enter is pressed', () => {
    const onSend = vi.fn()
    setup({ text: 'hello', onSend })

    const textarea = screen.getByRole('textbox')
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    expect(onSend).toHaveBeenCalledTimes(1)
  })

  it('does not call onSend when Shift+Enter is pressed', () => {
    const onSend = vi.fn()
    setup({ text: 'hello', onSend })

    const textarea = screen.getByRole('textbox')
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })

    expect(onSend).not.toHaveBeenCalled()
  })

  it('disables send button when text is empty', () => {
    setup({ text: '' })
    expect(screen.getByRole('button', { name: /发送/ })).toBeDisabled()
  })

  it('enables send button when text is non-empty', () => {
    setup({ text: 'hello' })
    expect(screen.getByRole('button', { name: /发送/ })).not.toBeDisabled()
  })

  it('shows stop button when busy', () => {
    setup({ busy: true })
    expect(screen.getByRole('button', { name: /停止/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /发送/ })).toBeNull()
  })

  it('calls onStop when stop button is clicked', () => {
    const onStop = vi.fn()
    setup({ busy: true, onStop })

    fireEvent.click(screen.getByRole('button', { name: /停止/ }))
    expect(onStop).toHaveBeenCalledTimes(1)
  })

  it('disables textarea when busy', () => {
    setup({ busy: true })
    expect(screen.getByRole('textbox')).toBeDisabled()
  })

  it('calls onChange and onTextareaChange when typing', () => {
    const onChange = vi.fn()
    const onTextareaChange = vi.fn()
    setup({ onChange, onTextareaChange })

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'hi' } })

    expect(onChange).toHaveBeenCalledWith('hi')
    expect(onTextareaChange).toHaveBeenCalledTimes(1)
  })

})
