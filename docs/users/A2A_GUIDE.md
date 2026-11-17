# 通过 A2A 让其他 Agent 调用 NekoCode

> **主要读者：** 启动和部署 NekoCode 的使用者。编写客户端见 [A2A 接入协议](../developers/A2A.md)，修改服务实现见 [A2A 维护说明](../maintainers/A2A.md)。

NekoCode daemon 可以作为 A2A 1.0 HTTP+JSON 服务，接收其他 Agent 发来的编码或研究任务。A2A 默认关闭，启动时传入 `-a2a` 才会开放协议端点和 Agent Card。

## 本地启用

先按 [用户指南](USER_GUIDE.md#二首次配置接入模型) 配置模型。daemon 读取与 TUI 相同的 `~/.nekocode/config.json`，工作区由启动目录决定。

从源码仓库根目录启动：

```bash
export NEKOCODE_DAEMON_TOKEN='replace-with-a-strong-token'
go run ./cmd/daemon -a2a -addr 127.0.0.1:8765
```

需要操作其他项目时，可以先在源码仓库构建 `go build -o nekocode-daemon ./cmd/daemon`，再进入目标项目目录，通过该二进制的绝对路径启动。

在另一个终端验证 Agent Card：

```bash
curl http://127.0.0.1:8765/.well-known/agent-card.json
```

Agent Card 公开，用于描述能力、协议地址和鉴权要求。客户端调用 `/a2a` 下的任务接口时，需要使用配置的 daemon token。完成一次请求的示例见 [发送文本任务](../developers/A2A.md#发送文本任务)。

如果使用 A2A CLI，先从 Agent Card 发现服务，再按所装版本的帮助配置认证和发送任务。CLI 的参数不由 NekoCode 定义，以该工具的 `help` 输出为准。

## 通过反向代理提供服务

假设客户端访问 `https://agent.example.com`，代理把请求转发到本机 `127.0.0.1:8765`：

```bash
export NEKOCODE_DAEMON_TOKEN='replace-with-a-strong-token'
go run ./cmd/daemon -a2a \
  -addr 127.0.0.1:8765 \
  -a2a-url https://agent.example.com/a2a
```

`-addr` 是监听地址，`-a2a-url` 是写入 Agent Card 的客户端访问地址。未指定后者时，daemon 使用首个本地访问地址加 `/a2a`；通过代理访问时应显式设置。

代理需要转发 `/.well-known/agent-card.json` 和 `/a2a/`，保留 `Authorization`、`A2A-Version` 和请求正文。流式输出及订阅使用 SSE，应关闭响应缓冲，并为长任务设置合适的连接超时。对外服务使用 HTTPS，token 通过请求头传递，不写进 Agent Card 或 URL。

需要直接监听网卡时可改用 `-addr 0.0.0.0:8765`。启用 A2A 后，任何非 loopback 监听地址都必须配置 token；未启用 A2A 时保留原 daemon 的监听行为。

## 任务等待输入时怎么办

任务状态为 `TASK_STATE_INPUT_REQUIRED` 时，查看状态消息中的工具审批或问题，完成回复后任务才会继续。A2A 客户端应使用原 Task ID 和 `interactionId` 回复，避免迟到回复作用于另一个请求，具体格式见 [处理审批和问题](../developers/A2A.md#处理审批和问题)。

也可以通过 daemon 原有的审批、回答和取消接口处理同一个运行。A2A 会同步后续状态和结果，客户端可以查询或重新订阅任务。

一个 daemon 共享一个 Runtime。A2A 执行期间，其他入口可能因 Runtime 忙而无法启动新运行；任务结束后恢复原活动会话，包括尚未保存为历史的空会话及其工作模式。A2A 暂不支持 Super Agent 模式，已有后台任务时也会拒绝切换到 A2A 会话。任务结束后若有会话清理或恢复告警，客户端仍应展示终态说明。

## 重新对话与重启服务

客户端通过 `contextId` 选择对话上下文。同一 Context 可以继续对话，新对话使用新的 Context；A2A 会拒绝 `/new`、`/fork` 和带参数的 `/sessions`，避免命令改变会话后仍沿用旧映射。普通 TUI、HTTP 等入口的命令行为保留。

daemon 重启后，内存中的 A2A Task 和 Context 映射不会恢复。即使磁盘上仍有 NekoCode 会话，也不能用旧 Task ID 继续审批或订阅；应发起新任务。会话数据的保存和清理见 [数据目录](DATA_DIRECTORY.md) 与 [会话指南](SESSION_GUIDE.md)。

## 按现象排查

| 现象 | 检查方式 |
| --- | --- |
| Agent Card 或 `/a2a` 返回 404 | 确认启动命令包含 `-a2a`，代理转发了对应路径 |
| Card 能访问，任务请求返回 401 | Card 本来就是公开的；核对调用请求的 Bearer token |
| 启动时报拒绝未认证监听 | 设置 token，或使用字面量 loopback 地址，如 `127.0.0.1:8765` |
| 客户端发现了无法访问的地址 | 核对 Card 中的 URL，设置客户端可访问的 `-a2a-url` |
| 任务没有继续，但连接已经结束 | 检查是否为 `INPUT_REQUIRED`，回复原 Task 后重新查询或订阅 |
| SSE 直到最后才显示结果 | 检查代理缓冲和连接超时；协议错误也可能在 SSE 内容中返回 |
| 提示忙或后台任务活跃 | 等待、完成交互或取消现有运行；A2A 不会绕过共享 Runtime 的运行限制 |
| Super Agent Context 无法继续 | 发送 `/mode standard` 或 `/mode plan` 恢复；A2A 拒绝切换到 Super Agent |
| 重启后旧 Task 查不到 | 使用新任务和新 Context；A2A 映射不跨进程恢复 |

模型、权限和扩展相关问题继续按 [故障排查指南](TROUBLESHOOTING.md) 定位。完整支持范围和请求限制见 [A2A 接入协议](../developers/A2A.md)。
