# Context Compaction Design — 五层压缩流水线

## 设计原则

1. **系统 prompt 绝对不可变。** 动态信息注入到用户消息或 `<system-reminder>` 标签，不嵌入 system prompt。
2. **从便宜到昂贵。** 每层都比上一层成本高。前一层解决问题就不进入下一层。
3. **信息守恒递减。** 从无损裁剪（截断）到有损压缩（LLM 摘要），越往后信息损失越大但压缩率越高。
4. **缓存感知。** 每层操作都考虑对 DeepSeek KV cache 前缀的影响，优先操作不影响活跃前缀的"冷"区域。

---

## 五层流水线

```
Layer 1: Tool Result Budgeting     → 入口裁剪（存储前）
Layer 2: History Sniping           → 切除冷历史（消息队列头部）
Layer 3: Microcompact              → 外科手术式清洗（工具结果内容）
Layer 4: Context Collapsing        → 轻量 LLM 局部提炼融合
Layer 5: Auto-Compaction           → 整体上下文重建
```

每层触发后重新估算 token。够用则停，不够则进入下一层。

---

## Layer 1: Tool Result Budgeting

**触发时机**：工具结果写入存储时（每次 `AddToolResult`）

**操作**：
- Read 结果 > 6K chars → 截断并追加 `[... truncated, N more chars]`
- Grep/Glob/List 结果 > 2K chars → 截断
- Bash 输出 > 6K chars → 截断
- 结果存储前检查最近 3 条已有消息，内容重合度 > 80% → 跳过存储

**设计意图**：在信息进入上下文之前就裁剪。一条 `cat` 了几万行的输出如果直接写进 `Messages`，会永久占据上下文直到被清除。入口裁剪保证进入上下文的每条信息都有体积上限。

**缓存影响**：无。截断发生在存储时，后续所有消息保持截断后的内容不变。

---

## Layer 2: History Sniping

**触发时机**：消息总数 > windowSize × 5（约 100 条），或每 8 轮强制检查

**操作**：
- 从消息队列**最前面**（最老的消息）开始裁剪
- 每次裁剪 N 条（N = windowSize），插入边界标记 `[History boundary — earlier messages snipped]`
- 被裁剪的消息位于 compactBoundary **之前**——它们早已不在 Build() 的可见窗口内
- 只裁剪**冷历史**：已经超出常用缓存范围、LLM 不再引用的极早期对话

**为什么不会破坏活跃缓存**：
- 被裁剪的消息已被 compactBoundary 排除在 Build() 输出之外
- 它们从未在最近的 LLM 请求中出现过
- DeepSeek 的活跃缓存前缀由最近轮次构成，冷历史不在其中
- 裁剪操作只影响 `Messages` 切片的头部，`CompactBoundary` 同步前移

**与 Layer 3 的区别**：
- History Sniping：**整条删除**消息（assistant + tool_result 一起删），消息数减少
- Microcompact：**保留消息结构**，仅清空工具结果**内容**，消息数不变

Layer 2 比 Layer 3 更彻底（直接删消息），但也更无损（被删的都是已经不参与 Build 的冷消息）。

---

## Layer 3: Microcompact

**触发时机**：上下文 > 50% token budget，且近期高强度代码调试导致消息快速膨胀

**操作**：外科手术式清洗——不触碰消息结构，只清除特定工具结果的内容：
- grep/list/glob/web_search 结果 → 优先清除（一次性导航辅助，过期后无引用价值）
- 旧 bash 输出（短命令、exit code） → 清除
- read/edit/write 结果 → 保留（跨轮次引用）
- subagent (task) 结果 → 保留（结构化产出，信息密度高）
- 最近 2 轮用户消息的工具结果 → 始终保留（LLM 正在使用）

替换内容为占位符：`[Old tool output cleared to save context space]`

**设计意图**：消息结构保留意味着 `filterValidMessages` 不会因配对丢失而丢弃整段对话。但工具输出的体积被消除——一条 grep 结果可能 3K+ token，清除后仅 20 字节。清除 30 条旧结果可以回收 ~50K+ token。

**缓存影响**：被清除的消息内容变了 → 当轮 Layer 1 缓存失效。但代价有限（一次 miss），收益巨大（后续每轮上下文缩小）。

---

## Layer 4: Context Collapsing

**触发时机**：上下文 > 70% token budget（Layer 2 + Layer 3 已执行但不够）

**操作**：轻量级 LLM 参与局部提炼融合。
- 不把整段消息历史丢给 LLM。只取 compactBoundary 之后、最近 3 轮之前的**中间段**
- LLM 接收这段消息 + 现有 Archive（如有），生成融合后的新 Archive
- Archive 包含被压缩消息的关键信息（文件路径、错误信息、决策结论）
- 融合逻辑：`LLM(旧 Archive + 中间段消息)` → 新 Archive（自动去重、合并、淘汰过时信息）

**与 Layer 5 的区别**：
- Context Collapsing：**局部**压缩中间段消息，保留 Head 和 Tail
- Auto-Compaction：**整体**重建，Head-Tail-Summary 完整流程

**缓存影响**：Archive 存入 Layer 0.5（Layer 0 和 Layer 1 之间）。两次 Collapsing 之间 Archive 字节完全不变 → 与 Layer 0 一起享受 DeepSeek 缓存命中。仅 Collapsing 触发时更新一次。

---

## Layer 5: Auto-Compaction

**触发时机**：上下文 > 85% token budget（前四层已用尽）

**操作**：使用 **Server-side Forking Call** 和 **Head-Tail-Summary Reconstruction** 技术进行整体上下文重建。

### Server-side Forking Call

不走常规的 `ChatStream` 路径（会触发工具调用、流式输出等完整流程）。而是 Fork 一个独立的、仅做摘要的 LLM 请求：
- `tools = nil`（禁止工具调用）
- `thinking = disabled`（不需要推理）
- 专用 system prompt：`CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.`
- 专门为压缩设计的小模型或低 temperature 配置

这个 Fork Call 独立于主对话流程——它不消耗主 agent 的上下文配额，不触发工具执行，不需要流式 StreamToken 解析。只是一个简单的 Chat 请求，拿到响应后提取摘要文本。

### Head-Tail-Summary Reconstruction

整个上下文被分解为三部分重建：

```
Head（保留）:  最近 N 轮完整消息（LLM 当前工作上下文，不压缩）
              保留标准：最近 3 轮用户消息 + 关联的 assistant/tool 消息

Tail（压缩）:  前 M% 的旧消息被 LLM 压缩为结构化摘要
              内容要求：完整代码片段、确切错误信息、文件路径 + 行号
              格式：<summary> + <key-facts>

构建产物（Archive）:
  Layer 0:    [sys] system prompt / tools / skills
  Layer 0.5:  [sys] [Archive] <Tail — LLM 压缩的旧消息摘要>
  Layer 1:    Head — 最近 N 轮完整消息
  Layer 2:    constraints / goal / key-facts / summary / todo / env
```

**质量检查**（已实现）：
- 摘要必须至少为输入大小的 10% 或 200 tokens（太小视为失败，扩大保留范围重试）
- 关键约束检查：用户硬约束（`<critical-constraints>`）在摘要中丢失 → 二次 LLM 调用补充

**缓存影响**：Auto-Compaction 后整个上下文重建。Layer 0 不变（命中），Layer 0.5 为新 Archive，Layer 1 为缩减后的 Head。当轮全 miss，但重建后的结构在后续轮次中重新稳定——Layer 0 + Layer 0.5 形成新的稳定前缀。

---

## 上下文分层（Build 输出）

```
Layer 0   (immutable):     [sys] system prompt
                           [sys] tools
                           [sys] skills

Layer 0.5 (semi-stable):   [sys] [Archive] <Tail — LLM 压缩的旧消息>

Layer 1   (history):       [user] msg_head_start
                           [assistant] resp_head_start
                           [tool] result_head_start
                           ...
                           [user] msg_latest

Layer 2   (volatile):      [sys] constraints
                           [sys] goal
                           [sys] key-facts
                           [sys] summary
                           [sys] todo
                           [user] [System] <env info>
```

---

## 缓存行为总结

| 操作 | 缓存影响 | 频率 | 代价评估 |
|------|----------|------|----------|
| Layer 1: Budgeting | 无影响 | 每次存储 | 零 |
| Layer 2: Sniping | 无影响（切的都是冷历史） | 消息 > 100 条 | 零 |
| Layer 3: Microcompact | Layer 1 部分 miss | 上下文 > 50% | 低（一次 miss） |
| Layer 4: Collapsing | Layer 0.5 miss，后续稳定命中 | 上下文 > 70% | 中（一次 LLM + 一次 miss） |
| Layer 5: Compaction | 整体 miss，后续全命中 | 上下文 > 85% | 高（一次 LLM + 一次全 miss） |

核心思想：**缓存 miss 的代价应该和操作的信息收益成正比。** Layer 1-2 零缓存代价处理大部分膨胀问题。Layer 3 以一次 miss 换取大量空间。Layer 4-5 以 LLM 调用 + 一次 miss 换取整体重建，是最昂贵的操作，仅在前面手段用尽时触发。
