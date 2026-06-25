// 匹配 Go common.DisplayMessage / session.Meta 序列化格式（无 json tag，Go 导出名 PascalCase）。

export interface SessionMeta {
  id: string
  cwd: string
  created_at: number
  updated_at: number
  msg_count: number
}

export interface DisplayMessage {
  Role: string
  Content: string
  Blocks: DisplayBlock[] | null
  Images: ImageRef[] | null
}

export interface DisplayBlock {
  ToolName: string
  Args: string
  Content: string
}

export interface ImageRef {
  Path: string
  URL: string
  Width: number
  Height: number
}

// 兼容旧命名。
export type SessionMessage = DisplayMessage
