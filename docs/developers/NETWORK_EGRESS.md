# Shell 网络域名限制

> 已实现 Linux 原生命名空间后端的 HTTP/HTTPS 出站域名限制。未指定域集合的旧网络策略保持原行为。

主要读者为修改权限、Shell 工具、沙箱或审批交互的开发者。日常使用入口见 [用户指南](../users/USER_GUIDE.md#shell-出站域名白名单)。

## 用户配置

在 `config.json` 的 `permissions.sandbox` 中为 Shell 规则配置 `outbound_domains`。例如：

```json
{
  "permissions": {
    "sandbox": {
      "shell(curl *)": {
        "sandbox_mode": "workspace-write",
        "outbound_domains": ["example.com"]
      }
    }
  }
}
```

这只设置执行环境与目标上限，不代替命令审批。Shell 工具也可按次提交 `outbound_domains` 参数；声明式规则与工具请求取交集，多条声明命中也取交集。精确域名不包括子域名；匹配前统一大小写、尾点与 IDNA。URL、IP、端口、通配符以及无效数组会被拒绝。

复合命令中只要任一子命令命中域名限制规则，该限制就作用于整次 Shell 调用；追加 `; echo done` 等其他命令不会移除限制。不同子命令命中的域名集合也取交集，交集为空时整次调用隔离断网。

| 域集合 | 行为 |
| --- | --- |
| 未提供或配置中为 null | 沿用 `network` 布尔策略 |
| 非空数组 | 仅通过代理访问集合中的 HTTP 80 / HTTPS 443 |
| 空数组 `[]` | 要求隔离断网，不能降级为普通执行 |

工具参数中的 null、字符串或混合类型数组会报错。提供域集合后，`network: true` 不能移除域限制；`sandbox_mode: host` 与域集合不能组合。已有广域网络 grant 也不会移除本次限制。

批准前，TUI、GUI、连接渠道的审批卡展示规范化后的域集合。选择“始终允许”只记住这个域集合的授权，后续扩大集合需要重新核验。授权记录使用 `net.outbound.domains` 标记，旧版本无法将它当作广域 `net.outbound` 授权使用。

## 支持边界与错误

- 第一版支持 Linux amd64/arm64 原生命名空间、HTTP 和 HTTPS CONNECT。amd64 的原生集成测试已执行；其他后端不支持域限制。
- HTTPS CONNECT 保持 TLS 端到端透传，不安装 CA，不解密内容。授权表示允许连接指定目标域的地址和 443 端口，不验证隧道内协议、SNI 或业务内容。
- 出站代理拒绝回环、私网、链路本地、未指定、多播及部分特殊转换地址。DNS 返回混合公私地址也会拒绝；校验后直接拨号选中的 IP，不重新解析。
- 不支持 UDP/QUIC、SSH、任意端口、本地服务或上游代理链。HTTP 重定向由客户端处理，后续请求重新核验域名。通过环境变量指向企业代理的旧配置不会被接入受限出口链。
- 不遵守 HTTP_PROXY/HTTPS_PROXY 的程序无法直接联网。设置 NO_PROXY、删除代理变量、直连 IPv4/IPv6、连接宿主 Unix socket 都不会获得其他出口。
- 命中未授权域时返回 HTTP 403 或 CONNECT 拒绝，包含目标域与原因；不会在代理内部自动弹出审批。命令可能将该错误转换为自己的退出码，curl 的 HTTP 请求应使用 `--fail` 让 HTTP 403 产生非零状态。
- 命名空间不可用时返回 `NetworkIsolationError`，不尝试 Landlock 或宿主执行。Landlock 的旧布尔策略只限制文件写入，不能当作网络隔离承诺。
- 为避免旁路，受限命令及后代不能创建 Unix socket、使用 io_uring、ptrace 或切换命名空间，并失去 namespace capabilities。依赖这些功能的程序可能不兼容，错误不会自动放宽限制。

## 实现与生命周期

```mermaid
flowchart LR
  Command[隔离网络中的命令] --> Bridge[命名空间内代理桥]
  Bridge --> Socket[专用 Unix socket]
  Socket --> Proxy[父进程域名代理]
  Proxy --> Target[获准域名的公共地址]
```

`runtime/netpolicy` 负责域名规范化、交集与授权包含关系。权限引擎、Shell 参数和持久化 grant 共用这些语义；nil 与非 nil 空数组必须区别保留。

原生沙箱始终为受限命令保留独立网络命名空间。父进程创建逐调用 Unix 代理；沙箱 PID 1 提供 loopback HTTP 代理桥，使用 bind-mounted Unix socket 联系父进程。命令通过自执行辅助入口降权并安装 seccomp 后运行，代理 socket 与辅助程序均只读挂载。PID 1 不可被命令 dump；命令不能创建 Unix socket 或窃取 supervisor 的描述符。

HTTP 代理保留原始 Host 头并与目标 authority 比较，拒绝不一致；每个客户端连接只处理一个 HTTP 请求或一个 CONNECT 隧道。代理不读取宿主的代理环境变量、不缓存跨调用权限；CONNECT 和桥接连接有五分钟连接期限，HTTP 响应头超时为三十秒。

同步 Run、异步 Start 都拥有自己的代理及 socket。取消、超时、启动失败和进程结束执行清理；命名空间 PID 1 退出时由内核杀掉剩余后代。子命令的正常非零退出码保持原值，不作为沙箱不可用处理。拒绝信息属于该次调用的输出，不保存 URL 路径、查询参数、请求体或认证头。

## 代码入口与依赖

| 职责 | 入口 |
| --- | --- |
| 域名规范化、交集和授权包含关系 | [runtime/netpolicy](../../bot/extension/tool/runtime/netpolicy/) |
| 配置校验、声明式规则、持久化授权 | [config.go](../../bot/config/config.go)、[permission](../../bot/extension/tool/runtime/permission/) |
| Shell 参数、有效域集合和执行 Profile | [args.go](../../bot/extension/tool/builtin/shell/args.go)、[profile.go](../../bot/extension/tool/builtin/shell/profile.go)、[tool_shell.go](../../bot/extension/tool/builtin/shell/tool_shell.go) |
| 预审批范围、权限重试和审批上下文转换 | [runner](../../bot/extension/tool/runtime/runner/) |
| HTTP/CONNECT 代理、DNS 校验和拨号 | [egress.go](../../bot/extension/tool/runtime/sandbox/impl/egress.go) |
| 命名空间初始化、代理桥、降权和 seccomp | [native.go](../../bot/extension/tool/runtime/sandbox/impl/native.go)、[egress_linux.go](../../bot/extension/tool/runtime/sandbox/impl/egress_linux.go) |
| 后端选择与禁止受限请求降级 | [linux.go](../../bot/extension/tool/runtime/sandbox/impl/linux.go)、[other.go](../../bot/extension/tool/runtime/sandbox/impl/other.go) |

实现复用 Go 标准库、`golang.org/x/net/idna` 和 `golang.org/x/sys/unix`，没有新增常驻系统服务，也不需要额外的 Node、bubblewrap、socat 运行依赖或 API Key。域集合在配置、工具参数、预审批和持久化授权之间使用相同语义；执行边界独立落实限制。

## 验证

```sh
go test -race ./bot/config ./bot/extension/tool/runtime/netpolicy ./bot/extension/tool/runtime/permission ./bot/extension/tool/runtime/runner ./bot/extension/tool/runtime/sandbox/impl ./bot/extension/tool/builtin/shell ./bot/core ./protocol ./interaction/tui/components ./interaction/connect
```

原生集成测试使用本地 HTTP/HTTPS 服务和仅测试代码可替换的解析/拨号函数，不放宽生产路径的私网限制。覆盖允许域、未授权域、混合公私 DNS、直连与 Unix socket 旁路、空集合、并发策略隔离、HTTPS 隧道、异步取消、退出码和禁止降级。user/network namespace 不可用时原生测试会跳过，跳过不能作为此功能的集成验收通过。

GUI 审批卡测试：在 `interaction/gui/web` 运行 `npm test -- src/components/__tests__/ConfirmDialog.test.tsx`，并运行 `tsc --noEmit`。

回退旧版本前，应显式移除或迁移使用 `outbound_domains` 的配置；旧配置解析器可能忽略该字段。保留授权文件备份，不把域名 grant 手工改成广域授权。
