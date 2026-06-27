import { LogoMark } from './LogoMark'

interface EmptyStateProps {
  onPromptSelect?: (prompt: string) => void
}

const promptStarters = [
  {
    label: '了解项目',
    prompt: '帮我定位这个项目的启动入口，并说明主要模块职责',
    tone: 'primary',
  },
  {
    label: '审查改动',
    prompt: '阅读当前工作区改动，按严重程度做一次代码审查',
    tone: 'success',
  },
  {
    label: '排查失败',
    prompt: '排查最近一次失败的测试或构建，并给出最小修复方案',
    tone: 'accent',
  },
]

export function EmptyState({ onPromptSelect }: EmptyStateProps) {
  return (
    <div className="flex min-h-[420px] w-full items-center justify-center px-4 pb-10 text-text-3">
      <div className="w-full max-w-[680px]">
        <div className="mb-6 flex items-start gap-4">
          <LogoMark size="lg" />
          <div className="min-w-0">
            <p className="mb-1 text-[11px] font-semibold uppercase tracking-[0.16em] text-primary">NekoCode workspace</p>
            <h2 className="text-[22px] font-semibold leading-tight text-text">从一个具体任务开始</h2>
            <p className="mt-2 max-w-[560px] text-[13px] leading-relaxed text-text-2">
              直接在底部输入，或选择一个模板填入输入框。发送第一条消息后，新的 session 会自动保存到左侧历史。
            </p>
          </div>
        </div>

        <div className="grid gap-2.5 sm:grid-cols-3">
          {promptStarters.map((item) => (
            <button
              key={item.label}
              type="button"
              onClick={() => onPromptSelect?.(item.prompt)}
              className="group min-h-[112px] rounded-md border border-border/50 bg-surface px-3.5 py-3.5 text-left transition-transform hover:-translate-y-0.5 hover:border-primary/50 hover:bg-surface-3 active:translate-y-0"
            >
              <span className={`mb-3 flex h-7 w-7 items-center justify-center rounded-md ${toneClass(item.tone)}`}>
                <SparkIcon />
              </span>
              <span className="block text-[12px] font-semibold text-text">{item.label}</span>
              <span className="mt-1.5 block text-[11px] leading-relaxed text-text-3 group-hover:text-text-2">
                {item.prompt}
              </span>
            </button>
          ))}
        </div>

        <div className="mt-5 flex flex-wrap items-center gap-2 text-[11px] text-text-3">
          <span className="rounded-md bg-surface px-2 py-1">Enter 发送</span>
          <span className="rounded-md bg-surface px-2 py-1">Shift + Enter 换行</span>
          <span className="rounded-md bg-surface px-2 py-1">历史会话会在左侧出现</span>
        </div>
      </div>
    </div>
  )
}

function toneClass(tone: string): string {
  switch (tone) {
    case 'success':
      return 'bg-success/14 text-success'
    case 'accent':
      return 'bg-accent/14 text-accent'
    default:
      return 'bg-primary/14 text-primary'
  }
}

function SparkIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" aria-hidden>
      <path d="M12 3 9.8 9.8 3 12l6.8 2.2L12 21l2.2-6.8L21 12l-6.8-2.2Z" />
    </svg>
  )
}
