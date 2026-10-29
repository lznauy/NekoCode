# NekoCode

<p align="center">
  <b>Your catgirl AI coding assistant &nbsp;·&nbsp; Open source under MIT &nbsp;·&nbsp; Runs from a single file</b>
</p>

<p align="center">
  <sub>Keyboard-friendly TUI · Cache reuse for lower costs · Parallel subtasks · Resumable sessions · On-demand extensions</sub>
</p>

<p align="center">
  <img src="docs/images/demo.gif" width="90%" alt="NekoCode demo">
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <b>English</b>
</p>

---

NekoCode is an open-source **AI coding workspace**. Describe a task, then let it read your project, edit code, run commands, and verify results. Larger tasks can use parallel subagents, with progress, usage, and file changes available for inspection.

Start with a terminal executable and a model configuration. Add skills, plugins, MCP services, and hooks as needed, or connect the same agent core to an editor, IM platform, or your own application.

## Why use NekoCode?

### Follow the work without losing your place

The TUI provides streaming answers, Markdown rendering, command completion, and menus. You can inspect tool activity, file changes, and task progress while handling approvals and questions in the same interface. Scrolling back preserves your reading position; `End` returns to the latest output. You can cancel a run or add instructions while it works.

Press `Ctrl+G` or enter `/tasks` to inspect subtasks, past attempts, final results, and paged tool output. The main conversation keeps a progress summary, with details available when you need them.

### Install one executable and configure your model

The TUI ships as a single executable. Basic use requires no frontend deployment or database server. Configure an API key, model ID, and endpoint, then use commands to switch models, select sessions, manage permissions, and install extensions.

Put project instructions in `NEKOCODE.md`. Project and user extensions have separate locations, and skill names follow a defined precedence order. Configure MCP services, language servers, and image models when you need them.

### Load long sessions on demand

Active model context and complete conversation history are stored separately. History is appended to disk and paged through an index; session restore reads the active context it needs. Run records and history caches have budgets, while large tool results use file references and paged reads. These mechanisms reduce the need to keep loading complete histories into memory.

Automatic summaries and `/compact` reduce active model context while preserving history for browsing and export. Actual memory use depends on context size, enabled extensions, and external tool processes; the design focuses on controlling resource growth during long sessions.

### Keep core instructions readable and workflows on demand

The [core prompt](bot/prompt/system_zh.md) keeps general collaboration rules in one place. `NEKOCODE.md` supplies project instructions, while skills hold workflows for debugging, planning, review, and other tasks. Full skill instructions load when needed, and `$skill-name` lets you request a workflow explicitly.

### Designed for cache hits and lower input costs

Multi-turn coding tasks repeatedly include project instructions, tool definitions, and conversation history. NekoCode assembles stable instructions separately from runtime metadata and keeps skill catalogs and tool definitions stable. MCP uses a proxy tool with a fixed schema, reducing tool-list changes as services connect or disconnect so providers can reuse matching request prefixes.

With a model service that offers cache discounts, cached input tokens use the provider's cache pricing, reducing repeated input costs during long conversations and tool loops. `/context` shows session and last-turn cache hit rates and usage so you can inspect the results. Actual hits and savings depend on the provider, model, cache lifetime, and request content.

### Built-in code index for symbols and call relationships

The `index` tool combines a bundled tree-sitter parser with a local SQLite index for Go, JavaScript/JSX, TypeScript/TSX, Python, and Rust. It answers symbol lookups, who calls a symbol and what it calls, package dependencies, and full-text search. The index is built when you open a project, persisted to `.nekocode/index.db`, and kept current by a file watcher that skips unchanged files by content hash. A project skeleton is added to the prompt at startup so the model sees the overall structure before it starts.

All of this ships in the same executable, with no language server or external indexing MCP required; configure LSP when you also need type-level navigation. Language coverage and index results depend on the parser support actually implemented.

### A sandbox that ships with the binary and degrades gracefully

Shell commands run in an isolated `workspace-write` sandbox by default: the host filesystem is read-only, while the workspace and authorized directories are writable. Commands or permission rules can select `read-only`, or `host` after explicit approval. Network access and extra writable directories are granted as separate capabilities and shown together with the command approval instead of prompting twice.

On Linux, NekoCode prefers its built-in user-namespace and mount isolation and does not need external components such as bubblewrap. When that path is unavailable it falls back to Landlock and explicitly exposes runtime dependencies such as `/nix/store`, which helps on NixOS and in restricted containers. Actual isolation strength depends on kernel support and the backend in use.

### One agent core for the terminal and for chat apps

The TUI, editor ACP, Headless, HTTP/SSE, and the Telegram, Feishu, QQ, and WeCom connectors all share the same agent capabilities rather than wrapping a CLI in a relay script. From chat you can approve actions, answer questions, cancel a run, and send more instructions, and the task center shows subtasks. The model can also use the `deliver` tool to send workspace artifacts back to the current channel, so discussion and execution happen in one place, whether you are following a long task from a phone or collaborating inside a company network.

### Use built-in tools with execution controls

Built-in tools cover file search, code reading, editing, shell commands, process management, and web research. Plan Mode supports reviewing an approach before implementation. Permission rules and sandboxing constrain execution, file guards check read evidence before changes, and checkpoints restore recorded file state by user message.

Connect model services through the implemented OpenAI or Anthropic protocol features and switch with `/model`. Sessions, checkpoints, and run records stay on your machine for backup, investigation, and integration with your own tools.

### Extend the workflow or build your own application

Use skills for workflows, custom agents for delegated tool scopes, hooks for lifecycle callbacks, and MCP for external tools. Package them as plugins for distribution. Claude Code-style extensions work within the supported compatibility subset.

Go developers can register tools, assemble a Runtime, or add interfaces and message channels. The `bot → runtime → interaction` layers separate inference, run management, and interaction code, with protocol references and tutorials in the repository.

## Main Features

| Feature | What you can do |
| --- | --- |
| Files and code | Read files and PDFs, write and edit files, preview diffs, browse trees, and search content; use code indexing and LSP with configured language services |
| Shell and processes | Run builds, tests, and development commands; inspect and manage processes started by tools |
| Web research | Search the web, fetch pages, and extract content alongside local code work |
| Parallel subagents | Delegate with separate tool scopes and contexts, queue work, inspect attempts, and resume saved tasks |
| Task lists and questions | Track pending and completed work, and answer clarification questions in the interface |
| Long sessions and project rules | Automatic or manual summaries, paged history, session restore, `NEKOCODE.md`, and project memory |
| Rewind and export | Restore recorded file checkpoints, export conversations, and retain local sessions and run records |
| Image understanding and generation | Analyze screenshots and images, or generate images through configured models; support varies by model and interface |
| File delivery | Send workspace artifacts through IM channels that support file delivery |
| Planning, approvals, and sandboxing | Review plans, approve operations, constrain workspace access, cancel runs, and add instructions |
| Models, caching, and usage | Configure and switch models, select supported reasoning levels, reuse stable prefixes, and inspect token usage and cache hits |
| Extensions | Skills, plugins, custom agents, MCP, and hooks; optional Jev-assisted decisions and summary relevance filtering |
| Code index | Built-in tree-sitter index for symbols, call relationships, package dependencies, and full-text search, updated incrementally on file changes |
| Multiple interfaces | Terminal TUI, editor ACP, Headless, HTTP/SSE, Telegram, Feishu, QQ, and WeCom |
| Embedding and observation | Runtime Go API, custom tools and channels, structured run events, diagnostic logs, and read-only hook inspection |

## Choose Your Interface

| Scenario | Entry point |
| --- | --- |
| Terminal development | `nekocode-tui`, with keyboard controls, streaming output, and a task center |
| Editor workflows | ACP v1 over stdio with a compatible client |
| Dispatch and follow tasks through IM | Telegram, Feishu, QQ, and WeCom connectors, within each platform's supported capabilities |
| Scripts and subprocess hosts | Headless bidirectional NDJSON with approvals, questions, session management, and run controls |
| Custom applications, interfaces, and services | Runtime Go API and HTTP/SSE |
| Desktop window | GUI in development, not yet included in releases |

## Quick Start

### Step 1: One-line install

```bash
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/lznauy/NekoCode/master/scripts/install.sh | sh
```

> The script auto-detects your OS (Linux / macOS) and architecture, and installs to `~/.local/bin`.
> It verifies the binary against the release `SHA256SUMS` file. For v0.4.2, it uses vetted SHA-256 values bundled with the installer.
> Windows users: run via [WSL](https://learn.microsoft.com/windows/wsl/install).

<details>
<summary>Other installation methods (manual download / build from source)</summary>

Download the binary for your platform from the [Releases page](https://github.com/lznauy/NekoCode/releases), add execute permission, and run it.

Or build from source (requires [Go 1.26.10+](https://go.dev/dl/)):

```bash
git clone https://github.com/lznauy/NekoCode.git
cd NekoCode
go build -o nekocode-tui ./cmd/tui
```

</details>

### Step 2: Configure your API key

```bash
mkdir -p ~/.nekocode
cat > ~/.nekocode/config.json << 'EOF'
{
  "active": "deepseek/deepseek-v4-flash",
  "providers": [
    {
      "name": "deepseek",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "sk-put-your-key-here",
      "protocol": "openai",
      "models": ["deepseek-v4-flash"]
    }
  ]
}
EOF
```

### Step 3: Start chatting

```bash
nekocode-tui
```

After configuration, start the TUI and enter a request. Common commands: `/plan <task>` to review an approach, `/tasks` to inspect subtasks, `/compact` to compact context, `/rewind` to restore file changes, `/sessions` to resume a session, and `/context` to inspect context and cache usage.

### Use as an ACP agent

NekoCode supports the stable Agent Client Protocol v1. Configure your editor to launch it with the project directory as its working directory:

```bash
nekocode-tui --acp
```

ACP mode communicates over stdio and does not start the TUI. Session configuration is scoped to the current ACP connection and never overwrites global user configuration. Client-supplied stdio MCP processes are disabled by default; use `nekocode-tui --acp --allow-client-mcp` only when the client and its workspace configuration are fully trusted. See [ACP.md](docs/developers/ACP.md) for capabilities and limits.

### Use as a Stream JSON subprocess

```bash
nekocode-tui -p "Explain this project" --output-format stream-json
nekocode-tui --headless
```

Supports NDJSON turns, tool events, approvals, questions, and cancellation. This is the
NekoCode-owned `nekocode-headless/2` protocol with session management, model selection,
checkpoint rewind, subagent output, steering, and graceful shutdown.
See [STREAM_JSON.md](docs/developers/STREAM_JSON.md) for the supported contract (Chinese).

## Documentation and Tutorials

Use the [documentation index](docs/README.md) to choose a reading path. Detailed guides are currently maintained in Chinese.

| Reader | Suggested guides |
| --- | --- |
| Users | [Installation and use](docs/users/USER_GUIDE.md), [troubleshooting](docs/users/TROUBLESHOOTING.md), [session recovery and backups](docs/users/SESSION_GUIDE.md), [release history](docs/users/CHANGELOG.md) |
| Extension authors | [Skills](docs/developers/SKILL_GUIDE.md), [custom agents](docs/developers/AGENT_GUIDE.md), [plugins](docs/developers/PLUGIN_GUIDE.md), [hooks](docs/developers/HOOK_GUIDE.md) |
| Application developers | [Custom tools](docs/developers/TOOL_GUIDE.md), [Runtime applications](docs/developers/RUNTIME_APP_GUIDE.md), [HTTP API](docs/developers/RUNTIME_HTTP_API.md), [Headless integration](headless/README.md) |
| Project contributors and maintainers | [Contributing](docs/policy/CONTRIBUTING.en.md), [architecture](docs/maintainers/ARCHITECTURE.md), [module boundaries](docs/maintainers/ARCHITECTURE_BOUNDARIES.md), [CI/CD](docs/maintainers/CI_CD.md), [official releases](docs/maintainers/RELEASING.md) |

## Current Scope

Releases provide Linux and macOS TUI binaries for amd64 and arm64. Linux requires a compatible glibc environment; Windows runs through WSL. The GUI remains in development. NekoCode is actively evolving and has not been validated for large production deployments.

The task center is currently available in the TUI and defaults to the current parent session. Saved tasks can be inspected and resumed with their session; waiting work held in memory is not restored across processes. Checkpoints cover files recorded by supported write paths, rather than a transaction across the whole workspace or external services.

Model, MCP, image, and interface capabilities depend on configuration and implemented protocol features. See [data storage](docs/users/DATA_DIRECTORY.md) and [image support](docs/users/IMAGES.md); future directions are collected in the [plan directory](docs/plan/README.md).

## Contributing

- [CONTRIBUTING.en.md](docs/policy/CONTRIBUTING.en.md): contribution guide, coding standards, and test requirements
- [CODE_OF_CONDUCT.md](docs/policy/CODE_OF_CONDUCT.md): community conduct expectations
- [GOVERNANCE.md](docs/policy/GOVERNANCE.md): maintainer and release responsibilities
- [SECURITY.md](docs/policy/SECURITY.md): how to report security vulnerabilities
- [SUPPORT.md](docs/policy/SUPPORT.md): supported environments and help channels

## License

NekoCode is available under the [MIT License](LICENSE).

## Star History

<a href="https://www.star-history.com/?repos=lznauy%2FNekoCode&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
   <img alt="NekoCode Star History Chart" src="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
 </picture>
</a>
