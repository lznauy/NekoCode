# 文档目录

文档按主要读者分为项目开发维护者、二次开发者和使用者三类；共享政策放在 `policy/`。项目介绍与快速开始见仓库首页：[简体中文](../README.md) · [English](../README.en.md)。

## 使用者（users）

安装、配置和使用 NekoCode，以及处理本机数据和日常故障。

| 文档 | 内容 |
| --- | --- |
| [用户使用指南](users/USER_GUIDE.md) | 安装、模型配置、日常使用、IM 和 MCP 接入 |
| [故障排查指南](users/TROUBLESHOOTING.md) | 启动、模型、扩展、权限与运行异常的定位步骤 |
| [会话恢复、回退与备份](users/SESSION_GUIDE.md) | 恢复会话、检查点、导出、备份和清理 |
| [数据目录](users/DATA_DIRECTORY.md) | 本机数据位置、磁盘布局和清理影响 |
| [图片支持](users/IMAGES.md) | 各入口的图片能力和传输约定，含接入者可参考的处理链路 |
| [更新日志](users/CHANGELOG.md) | 版本变化和升级影响，项目开发维护者也可查阅 |

## 二次开发者（developers）

基于 NekoCode 的公开能力编写扩展、工具或上层应用，以及对接编辑器和外部客户端。

| 文档 | 内容 |
| --- | --- |
| [自定义 Agent](developers/AGENT_GUIDE.md) | 定义工具范围、安装和委托子 Agent |
| [自定义 Skill](developers/SKILL_GUIDE.md) | 编写、调用和分享工作流 |
| [自定义 Plugin](developers/PLUGIN_GUIDE.md) | 组合、安装、更新和分发扩展 |
| [Hook 使用与开发](developers/HOOK_GUIDE.md) | 插件事件、JS 和命令动作、状态、诊断及 Go 接入 |
| [自定义工具开发](developers/TOOL_GUIDE.md) | 实现 Go 工具、编译接入宿主并验证 |
| [Runtime 应用开发](developers/RUNTIME_APP_GUIDE.md) | 组装自己的应用、UI 或消息渠道 |
| [ACP 协议](developers/ACP.md) | 编辑器协议、能力协商与实现状态 |
| [Headless 协议](developers/STREAM_JSON.md) | 双向 NDJSON 消息与控制契约（当前实现） |
| [Runtime HTTP API](developers/RUNTIME_HTTP_API.md) | HTTP/SSE 对外接口与事件契约 |

编写 Skill、Agent 或插件配置不要求修改 NekoCode 源码；编写 Go 工具或宿主应用需要相应的 Go 开发环境。各文档开头标注具体适用读者。

## 项目开发维护者（maintainers）

修改 NekoCode 内部实现、审查代码，以及维护流水线和发布官方版本。技术路线与未完成的方向记录在 `plan/`。

| 文档 | 内容 |
| --- | --- |
| [整体架构](maintainers/ARCHITECTURE.md) | 模块职责、Agent 循环和内部实现 |
| [架构边界](maintainers/ARCHITECTURE_BOUNDARIES.md) | 分层约束、状态所有权与协作约定 |
| [CI/CD 流程](maintainers/CI_CD.md) | 本仓库自动化检查、发布流水线和产物校验 |
| [发布操作指南](maintainers/RELEASING.md) | 准备、验证和发布 NekoCode 官方版本 |

参与项目开发前阅读 [贡献指南](policy/CONTRIBUTING.md)。二次开发者需要理解内部实现时，也可以按需查阅本组文档。

## 后续规划（plan）

只放还没做、准备做的方向、设计与取舍记录。实现完成后先修正对应现状文档，再把条目从 `plan/` 移除。目录约定见 [规划目录说明](plan/README.md)。

| 文档 | 内容 |
| --- | --- |
| [对比补齐方向](plan/COMPARISON_GAPS.md) | 相对主流产品的短板与补齐顺序 |
| [Headless 协议剩余差距](plan/STREAM_JSON_GAPS.md) | `nekocode-headless/2` 尚未暴露的能力与暂缓项 |
| [子任务后续](plan/SUBAGENT_NEXT.md) | 全局列表、删除、输出搜索导出、多端接入、worktree 隔离 |

当前行为以对应现状文档为准：Headless 见 [STREAM_JSON.md](developers/STREAM_JSON.md)，子任务见 [用户指南](users/USER_GUIDE.md#查看子任务) 和 [数据目录](users/DATA_DIRECTORY.md#subagents--子任务会话)。

## 共享政策（policy）

项目规则、参与方式、支持范围与安全政策，面向所有参与者。

| 文档 | 内容 |
| --- | --- |
| [贡献指南](policy/CONTRIBUTING.md) · [English](policy/CONTRIBUTING.en.md) | 贡献流程、开发要求与提交检查 |
| [行为准则](policy/CODE_OF_CONDUCT.md) | 社区参与和行为规范 |
| [项目治理](policy/GOVERNANCE.md) | 维护者职责与决策方式 |
| [安全策略](policy/SECURITY.md) | 安全支持范围与漏洞报告渠道 |
| [支持范围](policy/SUPPORT.md) | 支持版本、求助方式与问题反馈 |
| [版权与许可证](../LICENSE) | MIT 授权条款与版权声明，原文保留在仓库根目录 |

## 包内指南与归档约定

包内专用文档随代码维护：[Headless 接入](../headless/README.md)、[ACP 实现](../acp/README.md)、[构建说明](../build/README.md)。

- 按主要读者选择目录；同一文档同时适用多个角色时，在文首说明，不复制多份。
- 操作教程和协议说明可以属于同一读者目录。修改内部实现与官方发布归 `maintainers/`，扩展与应用接入归 `developers/`，产品使用与本机数据管理归 `users/`。
- 新增、移动文档时同步更新本目录、相对链接及代码和 CI 中的文件引用。
- 规划类内容只写在 `plan/`：其他文档描述当前实现，出现“后续会做”“计划支持”一类内容时，移入 `plan/` 并在这里补链接。
- 图片等共享资源保留在 `images/`；中英文项目首页和 `LICENSE` 保留在仓库根目录。
