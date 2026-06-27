import { useMemo, useState } from 'react'
import { safeReplyQuestion } from '../lib/wails'
import type { QuestionEvent } from '../types/events'

export interface QuestionEntry extends QuestionEvent {}

function QuestionDialog({
  entry,
  onDone,
}: {
  entry: QuestionEntry
  onDone: () => void
}) {
  const initial = useMemo(
    () => entry.questions.map((q) => (q.options?.[0]?.label ? [q.options[0].label] : [])),
    [entry.questions],
  )
  const [answers, setAnswers] = useState<string[][]>(initial)
  const [custom, setCustom] = useState<string[]>(() => entry.questions.map(() => ''))

  const submit = () => {
    const merged = entry.questions.map((q, i) => {
      const picked = [...(answers[i] ?? [])]
      const extra = custom[i]?.trim()
      if (q.custom && extra) picked.push(extra)
      return picked
    })
    safeReplyQuestion(entry.id, merged, false)
    onDone()
  }

  const reject = () => {
    safeReplyQuestion(entry.id, [], true)
    onDone()
  }

  return (
    <div className="fixed inset-0 z-[60] flex items-end justify-center bg-black/45 px-3 py-4 backdrop-blur-[2px] sm:items-center">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="question-title"
        className="flex max-h-[calc(100dvh-32px)] w-full max-w-2xl flex-col overflow-hidden rounded-lg border border-border/70 bg-surface-2 surface-shadow"
      >
        <header className="shrink-0 border-b border-border/45 px-4 py-3">
          <div className="mb-1 font-mono text-[11px] text-primary">question</div>
          <h2 id="question-title" className="text-[14px] font-semibold text-text">
            Agent needs your input
          </h2>
        </header>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-4">
          {entry.questions.map((q, index) => (
            <section key={`${q.question}-${index}`} className="space-y-3">
              <div>
                <div className="mb-1 text-[11px] font-medium uppercase tracking-wide text-text-3">
                  {q.header || `Question ${index + 1}`}
                </div>
                <div className="text-[13px] font-medium leading-relaxed text-text">
                  {q.question}
                </div>
              </div>

              {q.options && q.options.length > 0 && (
                <div className="space-y-2">
                  {q.options.map((option) => {
                    const active = answers[index]?.includes(option.label) ?? false
                    return (
                      <button
                        key={option.label}
                        type="button"
                        onClick={() => {
                          setAnswers((prev) => {
                            const next = prev.map((row) => [...row])
                            if (q.multiple) {
                              const row = new Set(next[index] ?? [])
                              if (row.has(option.label)) row.delete(option.label)
                              else row.add(option.label)
                              next[index] = [...row]
                            } else {
                              next[index] = [option.label]
                            }
                            return next
                          })
                        }}
                        className={`w-full rounded-md border px-3 py-2 text-left active:scale-[0.99] ${
                          active
                            ? 'border-primary/60 bg-primary/10 text-text'
                            : 'border-border/45 bg-surface hover:bg-surface-3/60 text-text-2'
                        }`}
                      >
                        <div className="flex items-start gap-2">
                          <span className="mt-0.5 font-mono text-[12px] text-primary">
                            {q.multiple ? (active ? '[x]' : '[ ]') : active ? '(*)' : '( )'}
                          </span>
                          <span className="min-w-0">
                            <span className="block text-[13px] font-medium">{option.label}</span>
                            {option.description && (
                              <span className="mt-0.5 block text-[12px] leading-relaxed text-text-3">
                                {option.description}
                              </span>
                            )}
                          </span>
                        </div>
                      </button>
                    )
                  })}
                </div>
              )}

              {q.custom && (
                <input
                  value={custom[index] ?? ''}
                  onChange={(e) => {
                    const value = e.currentTarget.value
                    setCustom((prev) => {
                      const next = [...prev]
                      next[index] = value
                      return next
                    })
                  }}
                  placeholder="Custom answer"
                  className="h-9 w-full rounded-md border border-border/45 bg-surface px-3 text-[13px] text-text outline-none focus:border-primary/60"
                />
              )}
            </section>
          ))}
        </div>

        <footer className="flex shrink-0 flex-col gap-2 border-t border-border/45 bg-surface-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-[12px] text-text-3">Your answer will be returned to the running tool call.</p>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={reject} className="secondary-button h-8 px-3">
              Dismiss
            </button>
            <button type="button" onClick={submit} className="primary-button h-8 px-3">
              Answer
            </button>
          </div>
        </footer>
      </section>
    </div>
  )
}

export default QuestionDialog
