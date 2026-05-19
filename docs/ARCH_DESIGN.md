# NekoCode Architecture Design

## 核心理念

> 上下文是稀缺资源。每一 byte 进入上下文都必须有明确理由。越是保持不变的内容，越靠近顶层；越是频繁变化的内容，越靠近底层。这是 KV cache 命中率最大化的唯一途径。

---

## 一、KV Cache 机制理解

DeepSeek 的磁盘 KV cache（默认开启，免费）有三个关键行为：

1. **前缀精确匹配。** 缓存单元是"从请求开头算起的完整前缀"。只有完全匹配一个已持久化的缓存单元时才能命中。部分重叠无效。

2. **三种持久化触发。**
   - 每次 HTTP 请求结束时，在用户输入结束位置和模型输出结束位置各持久化一个缓存单元。
   - 当系统检测到多个请求共享同一前缀时，将该公共前缀提升为独立缓存单元。
   - 长输入按固定 token 间隔自动快照。

3. **多轮对话自然缓存。** 第 1 轮：`[sys prompt + msg1 + response1]` 被持久化。第 2 轮：`[sys prompt + msg1 + response1 + msg2]` — 前缀到 response1 结束位置命中缓存，仅 msg2 是 miss。

**核心推论：请求开头的字节序列越稳定，缓存命中率越高。开头任何字节的变动都会使整个后续内容失效。**

---

## 二、上下文分层设计

### 分层原则

```
稳定性高的 → 放顶层（Layer 0）
稳定性中的 → 放中层（Layer 1）
稳定性低的 → 放底层（Layer 2）
```

- **Layer 0** 的内容在 session 生命周期内不变或极少变化。DeepSeek 在 1-2 轮后将其固化为公共前缀缓存单元。后续所有请求的 Layer 0 零成本。
- **Layer 1** 的内容随对话增长，但早期消息一旦确定就不再变更。多轮对话中，上一轮的完整 Layer 0+1 成为下一轮的前缀缓存。
- **Layer 2** 的内容每轮都可能变化，放在末尾不影响前缀缓存。

### 当前分层 vs 目标分层

**当前 Build 输出顺序：**

```
Layer 0: [sys] system prompt          ← 稳定
         [sys] anchor                 ← 半稳定（facts 在 summarization 后变）
         ─── 问题：anchor 变更导致 Layer 0 部分失效 ───

Layer 1: [user/assistant/tool]*       ← 逐轮增长

Layer 2: [sys] todo                   ← 可变
         [sys] skill list             ← 稳定 ← 位置错误！
         [sys] summary                ← 可变
         [sys] tool selection hint    ← 稳定 ← 位置错误！
```

两个稳定内容（skill list、tool selection hint）被放在可变区域，每次 todo 变化都跟着 miss。anchor 中的 `<key-facts>` 是可变内容却被放在稳定区域，summarization 后 facts 更新导致 Layer 0 全层失效。

**目标 Build 输出顺序：**

```
Layer 0: [sys] system prompt          ← 永不变化（session 内）
IMMUTABLE [sys] skill list             ← 永不变化（session 内）
PREFIX    [sys] constraints + goal     ← 极少变化（仅用户明确添加约束时）
          [sys] tool selection hint    ← 永不变化（固定文本）

Layer 1: [user] message 1             ← 一旦写入不再变更
MESSAGE   [assistant] response 1
HISTORY   [tool] result 1
          ...（逐轮增长）

Layer 2: [sys] key-facts              ← 可变（summarization 后更新）
VOLATILE  [sys] summary               ← 可变（compaction 后更新）
SUFFIX    [sys] todo                   ← 可变（每轮）
```

### 关键改动

1. **Anchor 拆分为二。** `<critical-constraints>` 和 `<current-goal>` 稳定（用户不主动加约束就不会变），放 Layer 0。`<key-facts>` 在每次 summarization 后可能新增，放 Layer 2。
2. **Skill list 和 tool selection hint 上移到 Layer 0。** 它们整个 session 不变。
3. **Layer 0 从 2 条消息变为 4 条。** 但这 4 条字节完全一致 → DeepSeek 将整个 Layer 0 作为一个公共前缀缓存单元。

### 命中率分析

```
请求 1（首轮）:
  输入：[Layer 0 (4 msgs)] + [Layer 1 (msg1)] + [Layer 2]
  缓存：请求结束时，Layer 0+msg1 被持久化为缓存单元
  命中：0%

请求 2：
  输入：[Layer 0 (4 msgs)] + [Layer 1 (msg1+resp1+msg2)] + [Layer 2]
  缓存：Layer 0+msg1+resp1 命中（请求 1 结束时持久化的前缀）
        msg2 是 miss（新内容）
  命中率：取决于 Layer 0+msg1+resp1 的 token 占比

请求 3：
  输入：[Layer 0 (4 msgs)] + [Layer 1 (msg1+resp1+msg2+resp2+msg3)] + [Layer 2]
  缓存：Layer 0+msg1+resp1+msg2+resp2 命中（请求 2 结束时持久化）
        msg3 是 miss

经过 1-2 轮后，DeepSeek 公共前缀检测将 Layer 0 提升为独立缓存单元。
即使后续 Layer 1 的消息历史因 compaction 而变更，Layer 0 仍然独立命中。
```

### 一次 compaction 后的缓存行为

当 compaction 触发，Layer 2 中的 summary 更新，`<key-facts>` 可能新增事实。此时：

```
compaction 后请求：
  输入：[Layer 0 (不变)] + [Layer 1 (compactBoundary 后移，早期消息被 summary 替代)]
        + [Layer 2 (summary+facts 已更新)]
  缓存：Layer 0 独立命中（已被公共前缀检测提升为独立单元）
        Layer 1 部分命中（compactBoundary 之后的消息未变）
        Layer 2 全 miss（内容都变了）
```

关键是 Layer 0 始终命中——它占总 token 的 ~5-10%，但稳定命中避免了整个前缀的级联失效。

---

## 三、Anchor 拆分设计

### 为什么必须拆分

`<key-facts>` 在 summarization 时可能新增事实（例如："项目使用 PostgreSQL 16"）。这一行文本的变更导致整个 Anchor 消息的字节序列改变 → Layer 0 缓存失效 → 后续所有消息全部 miss。

但 `<critical-constraints>` 和 `<current-goal>` 在用户不主动干预时完全不变。把它们和 facts 捆绑在一起是浪费缓存。

### 拆分方案

```
Layer 0 中的 stable-anchor：
  <critical-constraints>           ← 正则提取的用户硬约束（不变）
    - 不要修改 auth.go
    - 必须使用 OAuth 认证
  </critical-constraints>
  <current-goal>                   ← 从第一条用户消息提取（极少变）
    根据当前项目代码更新 docs 下的文档
  </current-goal>

Layer 2 中的 volatile-anchor：
  <key-facts>                      ← summarization 时提取（可能新增）
    - PostgreSQL 16 是主数据库
    - llm/ 支持 4 种 provider
  </key-facts>
```

### 为什么 constraints+goal 适合 Layer 0

- Constraints 只由用户消息触发提取（正则匹配），用户不主动加约束就不会变。
- Goal 只在用户发出新的实质性消息时更新，频率极低（一个 session 通常只有 1-2 个 goal）。
- 即使变化，也只在新的用户消息到达时变一次——变更成本可接受。

### 为什么 facts 适合 Layer 2

- Facts 在每次 summarization 时可能新增。
- 放在 Layer 2 意味着 facts 变更只破坏 Layer 2 缓存，不影响 Layer 0 和 Layer 1。
- Facts 的语义是"辅助记忆"而非"硬约束"，放末尾不影响模型遵循 constraints。

---

## 四、Subagent 设计原则

### 存在价值标准

一个 explore subagent 消耗 20-30K token。它的产出必须满足两个条件才有存在价值：

1. **信息密度高于主 agent 自己做同样的搜索。** 如果 explore 结果需要主 agent 重新 grep/read 才能用，subagent 就没有存在价值。
2. **可操作。** 主 agent 拿到结果后应该能直接生成 edit 调用，而非先"理解"再决定怎么做。

### 输出格式设计

**错误方向**（叙述式，不可操作）：
```
NekoCode is a Go terminal AI coding agent. It uses Bubble Tea for the TUI and
supports multiple LLM providers. The bot/ directory contains agent/, tools/,
ctxmgr/, skill/, prompt/ subdirectories...
```
这是一种"写给人看的总结"——主 agent 拿到后仍然需要自己 grep 确认每个声明。

**正确方向**（声明级验证，可操作）：
```
Claims checked:
- "bot/ 含 agent/ tools/ ctxmgr/ skill/ prompt/" → ls bot/ → TRUE
- "支持 Anthropic/OpenAI/GLM" → ls llm/ → PARTIAL (实际4个，文档说3个)
- "使用 Bubble Tea v2" → grep go.mod → TRUE (v2.2.1)
Discrepancies:
- llm/ 有 deepseek.go，文档未提及
```
每条声明带有验证工具、结果、布尔判定。主 agent 直接从 Discrepancies 字段提取 edit 指令——零额外搜索。

### 何时不用 Subagent

- 单文件验证 → 主 agent 直接 read
- 简单关键词搜索 → 主 agent 直接 grep
- 已知路径的 1-2 个文件 → 直接 read

Subagent 只用于：需要跨 3+ 目录搜索且结果需要交叉比对形成差异清单的场景。文档验证恰好符合这个条件——但前提是 subagent 产出的是差异清单，而非叙述文章。

---

## 五、上下文生命周期管理

### 分层清除策略

不同类型的结果有不同的引用价值和生命周期：

| 工具 | 信息密度 | 引用周期 | 截断上限 | 清除优先级 |
|------|----------|----------|----------|-----------|
| read | 高（完整文件内容） | 长（跨轮次引用） | 6K chars | 低（最后清除） |
| bash | 中（执行结果） | 中（当前任务内） | 6K chars | 中 |
| grep | 低（匹配行片段） | 短（一次性导航） | 2K chars | 高（优先清除） |
| glob/list | 低（目录列表） | 短（一次性导航） | 2K chars | 高（优先清除） |
| web_search | 低（搜索摘要） | 短 | 2K chars | 高 |
| task（subagent） | 高（结构化结果） | 长 | 不截断 | 低 |

清除时机：microCompact 触发时，按优先级从低到高清除。read 和 task 结果最后清除，grep/glob/list 优先清除。

### 去重原则

同一文件的同一行内容可能以多种形态存在（read 的行号文本、grep 的匹配行、bash cat 的原始输出）。存储新结果前，对比最近 3 条已有消息：如果内容片段重合度 > 80%，跳过存储或替换旧结果。

### Compaction 触发机制

1. **比例触发。** 上下文使用率超过 45% 时触发微压缩（清除工具结果），超过 60% 时触发完整摘要（LLM 压缩消息历史）。阈值与 token budget 成比例缩放——不再有"1M budget 下 95% 才触发"的问题。

2. **数量触发。** 可见消息超过窗口 3 倍（60 条）时，无论利用率多少都触发摘要——低利用率高轮次的隐性成本也需要控制。

3. **无进展止损。** 8 轮无编辑 → 注入强制推进提示。12 轮无编辑 → 强制 synthesize 并终止当前 run。这是代码硬止损，不依赖模型自觉。

---

## 六、系统 Prompt 设计原则

### 按任务组织，不按抽象规则堆砌

模型遵循"遇到 X 情况做 Y"远好于"遵循原则 Z"。

**反例**（当前结构）：
```
# Progressive Disclosure — 探索策略（20 行）
# Doing Tasks — 执行规则（15 行）
# Task Tracking — todo 使用（10 行）
# Sub-agents — 委派决策（40 行）
# Skills — 技能系统
# Honesty & Verification
# Safety
# Output Formatting — Markdown 安全（25 行）
规则散落在各处，模型需要在不同章节间跳转找相关规则。
```

**目标结构**（按任务组织）：
```
# Context Layout — 你看到的上下文结构
# Task Workflows
  ## Before Any Task: 读文件正确姿势
  ## Editing Code: Reproduce → Diagnose → Fix → Verify
  ## Verifying/Updating Docs: Read → Extract → Batch Verify → Edit → Report
  ## Exploring Unknown Code: Layer 1-2-3 → 决定自己做还是委派
# Tool Rules — 每个工具的硬规则（一处写清）
# Hard Constraints — 代码强制执行（标注为 enforced）
```

### 删除原则

- 每条规则必须能回答"没有它会发生什么具体问题"。答不上来的删除。
- 重复出现的规则合并到一处。同一信息出现在两个地方不会让模型更遵守，只会浪费 token。
- 角色扮演减到 1 行。成段的角色描述对编码任务无帮助。
- Markdown 安全规则从 prompt 移除，放到 post-processing 层（模型不需要理解 markdown 转义规则）。

### 硬约束标注

每条由代码强制执行的规则，标注为"代码保证"而非"建议"：

```
**Hard constraint (enforced by system):** 12 rounds without an edit → run terminates.
**Hard constraint (enforced by system):** Tool results > 6000 chars are truncated.
```

模型知道这些不是"建议"——做不到的时候系统会自动干预。这会减少模型"我知道应该做但懒得做"的侥幸心理。

---

## 七、设计原则清单

1. **缓存优先。** 稳定性从顶层到底层递减。Layer 0 任何变动都是昂贵的。
2. **信息密度优先。** Subagent 产出必须比主 agent 自己搜索更密集、更可操作。否则 subagent 没有存在价值。
3. **生命周期管理。** 每个进入上下文的内容都有明确的生存时间。grep 结果不需要存 10 轮。
4. **硬止损优先于软建议。** 如果某个行为很重要，用代码 enforce。Prompt 是辅助，不是保障。
5. **按任务组织优先于按抽象原则组织。** 模型面对具体场景时需要具体步骤，而非抽象规则。
6. **删除优先于新增。** 每加一条规则前，先找一条可以删除的。
