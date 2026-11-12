import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { TasksList } from './TasksList'

it('keeps blocked and cancelled work visible without counting it completed', () => {
  render(<TasksList todos={[
    { id: 'a', content: 'Inspect code', status: 'completed', result: 'Reviewed relevant paths' },
    { id: 'b', content: 'Run tests', status: 'blocked', reason: 'Test service unavailable' },
    { id: 'c', content: 'Publish', status: 'cancelled', reason: 'User cancelled publishing' },
  ]} />)
  expect(screen.getByText('1/3 · 33%')).toBeInTheDocument()
  expect(screen.getByText('受阻：Test service unavailable')).toBeInTheDocument()
  expect(screen.getByText('已取消：User cancelled publishing')).toBeInTheDocument()
  expect(screen.getByText('Reviewed relevant paths')).toBeInTheDocument()
})
