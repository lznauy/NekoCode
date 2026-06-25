// run 模块: 将 TUI 的 ProcessingItem 卡片在 Web 上以 React 组件重组。
//
// 一个 Run 卡片代表一次 assistant 的流式响应过程; 完成后仍保留为消息
// 的一部分, 内部子模块统一用 <details> 折叠, 不再清空。
//
// 子模块层级:
//   RunCard
//   ├─ RunHeader      spinner + phase 文案 + tokens + 工具计数
//   ├─ TasksList      Todo 进度 (折叠)
//   ├─ ActivityRow ×  工具步骤 (默认折叠持久工具)
//   ├─ ThinkingCard   (折叠)
//   └─ ImageGrid      生成的图片 (灯箱查看)
export { ImageGrid } from './ImageGrid'
export { RunCard } from './RunCard'
