# 图片支持（Image Attachments）

本文档描述 NekoCode 各接入面（TUI / GUI / IM 连接器 / headless / ACP / HTTP）的图片收发能力、
统一的协议约定，以及模型侧的消费路径。端到端各环节的代码位置见文末索引。

## 端到端管线

所有接入面共享同一条运行时管线——接入层只负责"落盘 + 填 `Input.Images`"，其余全部由 runtime 处理：

```
接入面：图片落盘为本地文件 + 填 Input.Images + 文本中带 [Image #N] marker
   ↓
runtime.StartRun（run goroutine 内执行，不占启动锁）
   adoptImageAttachments：把任意路径的图片复制进会话附件存储
   （~/.nekocode/tmp/images/<会话>/，该目录已授权为 session workspace root），
   改写路径、删除临时副本
   ↓
envelope 注入（runtime/input_attachments.go）
   指令按模型能力二选一：
   · vision 模型：条件式指令（直接看图；模型看不到时自动走工具）
   · 非 vision 模型：工具代理指令（用 image_understand 读取）
   ↓
provider 层按 EffectiveVision 分流：
   · vision=true  → 原生 image content parts 直达模型
   · vision=false → 剥离 Images（防发出模型无法解析的请求），工具兜底
```

vision 判定为动态三档：显式 config `vision` 字段 > 内置模型表（`glm-5`/`glm-4v`/`gpt-4*` 等）> false。
切换模型时 provider 与 envelope 同步重建，会话中途切换不断图。

## 统一约定

| 项 | 值 | 说明 |
|---|---|---|
| marker | `[Image #N]` | 必须出现在消息 Text 中，`ValidImageAttachments` 以此校验附件归属；全链路（TUI/GUI/连接器/headless/ACP/HTTP）唯一约定 |
| 单消息上限 | 8 张 | runtime 统一把关（`MaxImageAttachments`） |
| 单图上限 | 20 MiB | 附件存储与各连接器下载一致 |
| 能力门禁 | `ImageAttachmentsEnabled` | 模型支持 vision **或** 已配置 image_understand 模型，二者至少其一；否则带图输入被明确拒绝 |
| 生命周期 | 会话级 | 图片驻会话附件目录，模型可随时经 image_understand 重读；会话删除/过期自动清理 |
| tool 代理 | image_understand | 使用 config `image_understand_models`（fast/accurate 两档）读图并转为文字 |

## 各接入面能力矩阵

| 接入面 | 入站（图 → 模型） | 出站（模型 → 图） | 协议形态 |
|---|---|---|---|
| TUI | ✅ 终端粘贴 | ✅ | `Input.Images` |
| GUI (Wails) | ✅ `SendMessage` | ✅ | `Input.Images` |
| telegram | ✅ Photo 消息（getFile 下载） | ✅ sendPhoto；deliver_file → sendDocument | `Input.Images` |
| feishu | ✅ image_key 下载 | ✅ 图片消息；deliver_file → file 上传 | `Input.Images` |
| wecom | ✅ 回调 image.url 下载 | ⚠️ 平台无发图 API，文本保留路径降级 | `Input.Images` |
| qqbot | ✅ attachments[].url 下载 | ⚠️ 平台富媒体需公网 URL，同上降级 | `Input.Images` |
| headless | ✅ image block（路径引用） | ❌ 协议仅文本回传 | 本文档 §headless |
| ACP | ✅ image block（base64，按 ACP 规范） | ❌ | 本文档 §ACP |
| HTTP API | ✅ `images` 字段 | n/a（本地调试面） | 本文档 §HTTP |

wecom/qqbot 的出站降级是平台限制而非未实现：降级策略是结果文本中保留文件路径，不丢信息。

## 各接入面的图片协议

### headless（NDJSON over stdio）

用户消息的 `content` 数组支持三种 block，按出现顺序拼接：

```json
{"type": "user", "uuid": "...", "message": {"role": "user", "content": [
  {"type": "text",  "text": "看看这个"},
  {"type": "image", "path": "uploads/shot.jpg"}
]}}
```

- `path` 是**相对子进程工作目录**的路径（绝对路径也允许，但必须落在工作目录之内）；
  宿主（如 wecom-bot）应把下载的图片放进每会话工作区后以相对路径引用
- 服务端做双重防逃逸：词法 `filepath.Rel` 包含检查 + 文件存在时的 `EvalSymlinks`
  真实路径复检（`headless/server.go` `resolveWithinCWD`）——越界路径以 turn error 拒绝
- 每个图片 block 生成一个 `[Image #N]` marker（按 block 顺序）；纯图消息（无 text block）
  的文本就是 marker，与带文本消息向后兼容
- 纯字符串 `content`（既有宿主）完全不受影响

### ACP（编辑器接入）

`session/prompt` 的 content block 按 ACP 规范支持图片：

```json
{"type": "image", "data": "<base64>", "mimeType": "image/png"}
```

- base64 解码后直接 `attachment.SaveImage` 进**会话附件目录**（session root 已授权，
  image_understand 立即可读）——这就是解析必须在会话激活之后执行的原因
- 每个图片 block 生成 `[Image #N]` marker 拼进 prompt 文本
- 坏 base64 / 缺 data 以 JSON-RPC 参数错误（-32602）拒绝；存储失败报服务器错误（-32000）

### HTTP API

`POST /input` 请求体新增可选字段，语义与连接器约定一致：

```json
{
  "text": "看图 [Image #1]",
  "images": [{"label": "[Image #1]", "path": "/tmp/a.png"}]
}
```

`label` 必须出现在 `text` 中。HTTP API 默认绑定 `127.0.0.1`，路径语义与 `text`
等价（本地调试面），不做额外路径限制。

### IM 连接器（telegram / feishu / wecom / qqbot）

- **入站**：连接器下载平台短时效 CDN 图片到本地（telegram getFile / feishu image_key /
  wecom、qqbot 的临时 URL），经 marker 约定填 `Input.Images`，收养机制统一转存
- 下载在配对检查通过之后执行（不为陌生人拉取文件）；失败时回复明确的"图片下载失败"
  而非静默丢弃或内部错误
- **出站**：run 结果文本中引用的本地图片路径由 `connect.LocalImagePaths` 提取进
  `Intent.Images`；`Capabilities.Images` 决定渠道是否发原生图片。wecom/qqbot 不实现
  该能力，文本路径即降级
- **deliver_file 工具**：模型可主动把工作区文件（≤50 MiB）经 telegram sendDocument /
  feishu file 消息投递给用户；wecom/qqbot 平台无对应 API。投递目标由 `connector`
  参数指定（缺省广播所有支持文件的运行中连接器）

## 已知边界

- steer（运行中追加输入）只携带文本：图片仅在 run 启动时进入；steer 链不做原生注入，
  envelope 必须用工具代理指令
- 会话中途切到非 vision 模型：envelope 条件式指令自动引导新模型走 image_understand，
  历史图片路径始终可达
- 第三方网关的多模态兼容性（如经 copilot 代理的模型偶发丢弃原生图片）属于服务端问题；
  工具代理路径是稳定兜底

## 代码索引

| 环节 | 位置 |
|---|---|
| 收养（任意路径 → 会话附件存储） | `runtime/input_attachments.go` `adoptImageAttachments`（调用点 `runtime/runtime_run.go`） |
| envelope 注入与指令 | `runtime/input_attachments.go` `InputWithImageAttachments` |
| vision 判定 | `bot/config/modelmeta.go` `EffectiveVision`（provider 构造 `bot/core/bot.go` `initAgent`） |
| 原生部件编码 | `bot/provider/openai/client.go` `imageContentParts`；`bot/provider/anthropic/client.go` 同构 |
| 附件存储 | `util/attachment/image.go`（`SaveImage`/`ImageDir`/会话清理） |
| 连接器出站提取 | `interaction/connect/images.go` `LocalImagePaths` / `Intent.Images`（`connect/translate.go`） |
| headless 协议 | `headless/types.go` `frame.user`；`headless/server.go` `resolveWithinCWD` |
| ACP 协议 | `acp/content.go` `promptContent`（调用点 `acp/prompt.go`，会话激活后） |
| HTTP API | `runtime/httpapi/httpapi.go` `submitRequest.Images` |
| deliver_file 工具 | `bot/extension/tool/builtin/deliver/`（接口 `protocol.FileDeliverer`，路由 `runtime/internal/connectors/connectors.go`） |
