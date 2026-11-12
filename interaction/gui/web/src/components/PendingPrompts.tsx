export function PendingPrompts({ count = 0, previews = [] }: { count?: number; previews?: string[] }) {
  if (count <= 0) return null
  return <div className="mb-2 text-xs text-text-3" role="status">
    <p>还有 {count} 项待处理</p>
    {previews.length > 0 && <ol aria-label="后续待处理请求" className="mt-1 space-y-1">
      {previews.map((preview, index) => <li key={index} className="truncate">{index + 1}. {preview}</li>)}
    </ol>}
    {count > previews.length && previews.length > 0 && <p>另有 {count - previews.length} 项</p>}
  </div>
}
