# A2A 客户端接入协议

> **主要读者：** 对接 NekoCode 的 Agent 和应用开发者。启动与部署见 [使用者指南](../users/A2A_GUIDE.md)，修改适配器见 [维护说明](../maintainers/A2A.md)。

NekoCode 提供 A2A 1.0 HTTP+JSON 服务。支持文本任务、流式输出、查询、订阅、取消，以及审批和问题回答。文件 Part、Push Notification、gRPC、JSON-RPC、多租户和 Agent Card 签名未启用。

## 发现服务与认证

`GET /.well-known/agent-card.json` 是公开端点。读取其中 `supportedInterfaces` 提供的协议地址；默认部署为 `/a2a`，代理部署以 Card 公布的 URL 为准。

下面的示例使用 `http://127.0.0.1:8765/a2a`。请求头约定：

```text
Authorization: Bearer <daemon-token>
A2A-Version: 1.0
Content-Type: application/a2a+json
```

未配置 token 的 loopback 服务不要求 `Authorization`。A2A 沿用 daemon 的认证策略：GET 可使用其支持的查询 token，但客户端应优先使用请求头；POST 不接受查询 token。`-cors` 沿用只读 GET/OPTIONS 策略，不会为浏览器开放跨域 POST。

| 方法 | 默认路径 | 用途 |
| --- | --- | --- |
| POST | `/a2a/message:send` | 发送任务或交互回复 |
| POST | `/a2a/message:stream` | 发送消息并接收 SSE 事件 |
| GET | `/a2a/tasks` | 列出保留的 Task |
| GET | `/a2a/tasks/{id}` | 查询一个 Task |
| POST | `/a2a/tasks/{id}:subscribe` | 订阅 Task 快照 |
| POST | `/a2a/tasks/{id}:cancel` | 取消 Task |

## 发送文本任务

在配置了与服务端相同 token 的终端执行：

```bash
curl http://127.0.0.1:8765/a2a/message:send \
  -H "Authorization: Bearer $NEKOCODE_DAEMON_TOKEN" \
  -H 'A2A-Version: 1.0' \
  -H 'Content-Type: application/a2a+json' \
  -d '{
    "message": {
      "messageId": "request-1",
      "role": "ROLE_USER",
      "parts": [{"text": "解释这个项目的架构"}]
    }
  }'
```

响应的 `task` 对象包含 `id`、`contextId`、`status` 和已有产物。后续请求使用响应中的实际 ID。

默认 `message:send` 等待本次执行结束或需要输入。长任务可以在请求顶层增加 `"configuration": {"returnImmediately": true}`，先取得 Task，再查询或订阅；立即返回不表示运行完成。

| Task 状态 | 客户端下一步 |
| --- | --- |
| `TASK_STATE_SUBMITTED` / `TASK_STATE_WORKING` | 查询或订阅进度 |
| `TASK_STATE_INPUT_REQUIRED` | 展示交互请求，向原 Task 回复 |
| `TASK_STATE_COMPLETED` | 读取产物和状态说明，检查会话恢复告警；下一轮在原 Context 中创建新 Task |
| `TASK_STATE_FAILED` / `TASK_STATE_CANCELED` | 展示状态与错误，核对已经发生的修改，再决定是否重试 |

## 继续对话或开启新对话

| 目的 | 消息中携带的 ID |
| --- | --- |
| 开启新对话 | 省略 `taskId` 和 `contextId`，使用响应分配的 Context |
| 在已有对话中发起下一轮任务 | 携带原 `contextId`，省略 `taskId`，使用新的 `messageId` |
| 回答当前任务的审批或问题 | 携带原 `taskId` 和 `contextId`，使用新的 `messageId` |

比如第一轮已完成，继续同一对话时向 `message:send` 发送：

```json
{
  "message": {
    "messageId": "request-2",
    "contextId": "context-id",
    "role": "ROLE_USER",
    "parts": [{"text": "继续检查刚才提到的模块"}]
  }
}
```

Task 与 Runtime Run 的对应关系由适配器管理，A2A Task ID 不能直接当作原生 HTTP API 的 Run ID 使用。Task 和 Context 映射不跨 daemon 重启恢复。Task 淘汰后可能查不到；Context 淘汰或轮换后，即使沿用同一 `contextId`，也可能新建 Session，不能将该 ID 当作永久历史恢复凭据。

Context 分隔对话记录，仍共享工作区和 daemon token 的权限范围，不构成租户隔离。

A2A 会话由 `contextId` 管理。`/new`、`/fork` 和带参数的 `/sessions` 会被明确拒绝，新对话使用新 Context。`/mode super-agent` 会被拒绝；已有 Super Agent Context 可通过 `/mode standard` 或 `/mode plan` 恢复。

## 处理审批和问题

当 `task.status.state` 为 `TASK_STATE_INPUT_REQUIRED`，读取 `task.status.message.parts`。其中的文本用于展示，Data Part 用于识别交互：

| `data.type` | 其他数据 | 回复方式 |
| --- | --- | --- |
| `approval` | `interactionId`、`toolName`、`args` | 文本 `allow` / `deny`，或 `{"allowed": true/false}` |
| `question` | `interactionId`、`questions` | 按问题顺序提供 `answers`，或设置 `rejected` |

每次回复都必须携带原请求的 `interactionId`，放在 Data Part 或消息 `metadata` 中。缺少 ID、ID 过期或两处 ID 冲突时，任务继续等待输入，不会把回复应用到下一个审批或问题。

先向用户展示原始请求，再提交对应回复。以下 JSON 是发往 `message:send` 的完整审批回复，Task、Context 和交互 ID 均应替换为原响应的值：

```json
{
  "message": {
    "messageId": "approval-reply-1",
    "taskId": "task-id",
    "contextId": "context-id",
    "role": "ROLE_USER",
    "parts": [{"data": {"interactionId": "approval-id", "allowed": true}}]
  }
}
```

问题回复采用相同的消息结构，将 `parts` 换成：

```json
[
  {
    "data": {
      "interactionId": "question-id",
      "answers": [["第一个问题的回答"], ["第二个问题的回答"]],
      "rejected": false
    }
  }
]
```

文本 `allow` / `deny` 或单个问题的文本回答，需要在消息中同时设置 `"metadata": {"interactionId": "原交互 ID"}`。多个问题应使用结构化 `answers`。终态 Task 不接受续跑，继续对话时应在同一 Context 中创建新 Task。

同一个运行也可能由 daemon 原有接口批准、回答或取消。A2A 会跟踪后续状态；客户端不能把一次连接结束当作运行已经结束。

## 接收流式结果与订阅

首次发送任务时，使用 `message:stream`，并为 curl 增加 `-N` 关闭客户端输出缓冲。该端点发送 A2A 状态和 Artifact 事件；Artifact 更新的 `append` 为 false 时替换原内容，为 true 时追加。收到文本撤回后的替换事件时，应移除先前的推测文本。

对已有 Task 可发送：

```bash
curl -N -X POST 'http://127.0.0.1:8765/a2a/tasks/task-id:subscribe' \
  -H "Authorization: Bearer $NEKOCODE_DAEMON_TOKEN" \
  -H 'A2A-Version: 1.0'
```

订阅返回完整 Task 快照，包含当前累积产物。用新快照替换本地 Task，避免把同一段内容重复追加。订阅在完成、失败、取消或再次需要输入时结束；处理交互后可以重新订阅，外部入口解决交互后的后台续跑也支持订阅。

SSE 开始后，协议错误可能写在事件的 `error` 中，即使 HTTP 状态为 200 也需要检查事件正文。`message:send` 返回 Task 时同样应检查其状态，不能仅凭 HTTP 200 判定任务成功。

普通回答产物名为 `result`，完成时会用最终输出校正确认内容，不能把早期 delta 当作最终答案。`/help` 等控制命令的系统输出会作为独立的 `system` Artifact 返回。会话清理或恢复的告警也会进入 `system` 产物及终态消息，任务完成后仍应读取这些说明。

## 请求与运行边界

| 项目 | 当前边界 |
| --- | --- |
| 输入 | 文本 Part，以及审批和问题回复的 Data Part |
| 文本 / Data Part | 累计文本最大 1 MiB，每个 Data Part 最大 1 MiB |
| HTTP 请求体 | 最大 8 MiB |
| ID | Task、Context、Message ID 各最大 256 字节 |
| 并发 | 一个 Runtime 同时执行一个前台任务；并发 A2A 执行会被拒绝 |
| 工作模式 | A2A 不运行 Super Agent；已有后台任务时拒绝切换会话 |
| 重启与保留 | Task 和 Context 映射只保存在内存中，不提供跨进程恢复 |

客户端应展示失败原因并由调用方决定重试，特别是可能已经产生文件修改的任务。存储超限会使任务失败，保留内容可能被缩减以完成失败清理；不应把部分输出解释为完整结果。内存保留和会话清理的具体参数见 [维护说明](../maintainers/A2A.md#保留与清理)。
