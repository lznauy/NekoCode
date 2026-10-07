# A2A 接入

NekoCode daemon 提供 Agent2Agent（A2A）1.0 HTTP+JSON 服务，可被其他兼容 A2A 的 Agent 发现和调用。

## 启动

```bash
export NEKOCODE_DAEMON_TOKEN='replace-with-a-strong-token'
go run ./cmd/daemon -addr 127.0.0.1:8765
```

默认公布的 A2A 地址是首个本地访问地址加 `/a2a`。经过反向代理或使用公网域名时，必须显式设置外部地址：

```bash
NEKOCODE_DAEMON_TOKEN='replace-with-a-strong-token' go run ./cmd/daemon \
  -addr 0.0.0.0:8765 \
  -a2a-url https://agent.example.com/a2a
```

不要把 token 写入 Agent Card 或 URL。Agent Card 只声明 Bearer 鉴权方式。
监听 `0.0.0.0`、`::` 或空主机地址时，daemon 会强制要求配置 token。

## 端点

Agent Card 是公开端点：

```text
GET /.well-known/agent-card.json
```

A2A 操作挂载在 `/a2a`：

```text
POST /a2a/message:send
POST /a2a/message:stream
GET  /a2a/tasks
GET  /a2a/tasks/{id}
POST /a2a/tasks/{id}:cancel
POST /a2a/tasks/{id}:subscribe
```

除 Agent Card 外，端点沿用 daemon 的 Bearer Token。示例：

```bash
curl -X POST http://127.0.0.1:8765/a2a/message:send \
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

未配置 daemon token 的本地开发环境也可以使用官方 A2A CLI：

```bash
a2a discover http://127.0.0.1:8765
a2a send http://127.0.0.1:8765 "解释这个项目"
```

CLI 参数可能随版本变化，请以 `a2a help` 为准。

## 交互请求

NekoCode 请求工具审批或补充信息时，Task 会进入 `TASK_STATE_INPUT_REQUIRED`。状态消息同时包含：

- 一个可读的文本说明；
- 一个结构化 Data Part，包含 `type`、`interactionId` 以及审批或问题内容。

审批可以发送文本 `allow` / `deny`，也可以发送：

```json
{"allowed": true}
```

问题回答建议使用 Data Part：

```json
{
  "answers": [["第一个问题的回答"], ["第二个问题的回答"]],
  "rejected": false
}
```

回复必须携带原 Task ID；运行会从中断位置继续。

## 当前限制

- 只接受 `text/plain` 和审批/提问的结构化 Data Part。
- 单条文本或结构化 Data Part 最大 1 MiB；请求体最大 8 MiB，单个内存 Task 最大保留 16 MiB 和 100 条历史消息，Task Store 总保留量最大 64 MiB。
- Runtime 同一时刻只执行一个任务，其他并发 A2A 执行会立即被拒绝。A2A 会原子切换到对应 Context 的 Session，并在任务结束后恢复 daemon 原 Session。
- Task Store 和 A2A 到 Runtime 的映射保存在内存中，daemon 重启后不会恢复。默认保留最近 256 个 Task 和 64 个 Context；Context 达到 128 轮或累计 1 MiB 输入后轮换 Session，达到总量上限时淘汰最旧的终态记录及其专属 Session。Session 删除失败时会在后续启动任务前重试；清理积压达到上限后暂停创建新 Session。
- 尚未启用 Push Notification、文件 Part、gRPC、JSON-RPC、多租户和 Agent Card 签名。
- Agent Card 公开，但 A2A 操作在配置 token 时必须鉴权。生产部署应使用 HTTPS。
