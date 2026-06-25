import { safeReplyConfirm } from '../lib/wails'

export interface ConfirmEntry {
  id: string
  toolName: string
  args: Record<string, unknown>
  level: number
}

function ConfirmDialog({
  entry,
  onDone,
}: {
  entry: ConfirmEntry
  onDone: () => void
}) {
  const handle = (ok: boolean) => {
    safeReplyConfirm(entry.id, ok)
    onDone()
  }

  const levelLabel = ['safe', 'modify', 'danger', 'blocked'][entry.level] || '?'
  const levelColor =
    entry.level >= 3
      ? 'text-danger'
      : entry.level >= 2
        ? 'text-warning'
        : 'text-primary'

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-full max-w-md rounded-xl border border-border/70 bg-surface-2 p-5 surface-shadow animate-slide-in">
        <h2 className="text-sm font-semibold text-text mb-1">确认操作</h2>
        <div className="flex items-center gap-2 mb-3">
          <span className={`text-[11px] font-medium uppercase tracking-wide ${levelColor}`}>
            {levelLabel}
          </span>
          <span className="font-mono text-[13px] text-text">{entry.toolName}</span>
        </div>

        {Object.keys(entry.args).length > 0 && (
          <div className="mb-4 space-y-1.5">
            {Object.entries(entry.args).map(([k, v]) => (
              <div key={k} className="text-[11px]">
                <span className="text-text-3">{k}</span>
                <pre className="mt-0.5 max-h-[120px] overflow-auto whitespace-pre-wrap rounded-md bg-surface px-2 py-1.5 font-mono text-[11px] text-text-2 leading-relaxed">
                  {String(v ?? '')}
                </pre>
              </div>
            ))}
          </div>
        )}

        <div className="flex gap-2 justify-end">
          <button
            type="button"
            onClick={() => handle(false)}
            className="rounded-md border border-border px-4 py-2 text-[12.5px] text-text-2 transition-all hover:bg-surface-3 hover:text-text active:scale-95"
          >
            取消
          </button>
          <button
            type="button"
            onClick={() => handle(true)}
            className="rounded-md bg-primary px-4 py-2 text-[12.5px] font-semibold text-black transition-all hover:brightness-110 active:scale-95"
          >
            确认
          </button>
        </div>
      </div>
    </div>
  )
}

export default ConfirmDialog
