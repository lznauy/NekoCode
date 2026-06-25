export function EmptyState() {
  return (
    <div className="flex flex-1 items-center justify-center pb-16 text-text-3">
      <div className="w-full max-w-[520px] rounded-xl border border-border/70 bg-surface/80 p-6 surface-shadow">
        <div className="mb-4 flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-sm font-bold text-black">
          N
        </div>
        <h2 className="text-base font-semibold text-text">开始一个工程会话</h2>
        <p className="mt-2 text-[13px] leading-relaxed text-text-2">
          描述要修改、排查或构建的内容。运行过程、工具调用和结果会按时间线显示在这里。
        </p>
      </div>
    </div>
  )
}
