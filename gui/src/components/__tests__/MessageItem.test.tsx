import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { MessageItem } from '../MessageItem'
import type { Msg } from '../../types/events'

const toggleStep = vi.fn()
const userMsg: Msg = { id: '1', role: 'user', text: 'Hello world', streaming: false }
const assistantMsg: Msg = { id: '2', role: 'assistant', text: 'Hi there', streaming: false }
const toolMsg: Msg = { id: '3', role: 'tool', text: '🔧 `ls`\n\n```\nfile.txt\n```', streaming: false }
const streamingMsg: Msg = { id: '4', role: 'assistant', text: 'Thinking...', streaming: true }

describe('MessageItem', () => {
  it('renders user message text in a bubble', () => {
    render(<MessageItem msg={userMsg} toggleStep={toggleStep} />)
    expect(screen.getByText('Hello world')).toBeInTheDocument()
  })

  it('renders legacy assistant message text (fallback path, no Run metadata)', () => {
    render(<MessageItem msg={assistantMsg} toggleStep={toggleStep} />)
    expect(screen.getByText('Hi there')).toBeInTheDocument()
  })

  it('renders tool message with capitalized label', () => {
    render(<MessageItem msg={toolMsg} toggleStep={toggleStep} />)
    expect(screen.getByText('tool')).toBeInTheDocument()
    expect(screen.getByText(/ls/)).toBeInTheDocument()
  })

  it('shows streaming glyph when assistant fallback is streaming', () => {
    render(<MessageItem msg={streamingMsg} toggleStep={toggleStep} />)
    // StreamGlyph renders a filled "●" text node while streaming.
    expect(screen.getByText('●')).toBeInTheDocument()
  })

  it('does not show streaming glyph when not streaming', () => {
    render(<MessageItem msg={assistantMsg} toggleStep={toggleStep} />)
    expect(screen.queryByText('●')).toBeNull()
  })

  it('renders markdown content', () => {
    const mdMsg: Msg = { id: '5', role: 'assistant', text: '**bold** and `code`', streaming: false }
    render(<MessageItem msg={mdMsg} toggleStep={toggleStep} />)
    expect(screen.getByText('bold')).toBeInTheDocument()
    expect(screen.getByText('code')).toBeInTheDocument()
  })

  it('renders RunCard when assistant message has Run metadata (steps)', () => {
    const runMsg: Msg = {
      id: '6',
      role: 'assistant',
      text: 'hello',
      streaming: true,
      phase: 'thinking',
      steps: [],
    }
    const { container } = render(<MessageItem msg={runMsg} toggleStep={toggleStep} />)
    // RunCard shows an active phase label and a rotating spinner glyph while streaming.
    // eslint-disable-next-line testing-library/no-container
    expect(container.querySelector('svg.animate-spin')).toBeTruthy()
    expect(screen.getByText('思考中')).toBeInTheDocument()
  })
})
