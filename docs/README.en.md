# NekoCode

<p align="center">
  <b>Your catgirl AI coding assistant &nbsp;·&nbsp; Open source under MIT &nbsp;·&nbsp; Runs from a single file</b>
</p>

<p align="center">
  <sub>Parallel subtasks · Resumable long sessions · Checkpoint rewind · Terminal, editor, and IM access</sub>
</p>

<p align="center">
  <img src="images/demo.gif" width="90%" alt="NekoCode demo">
</p>

<p align="center">
  <a href="../README.md">简体中文</a> · <b>English</b>
</p>

---

## What is this?

NekoCode is an open-source **AI coding workspace**. Beyond reading code, editing files, and running tests, it can split larger requests into persistent subtasks and work through a terminal, an editor, IM platforms, or your own application.

### More than chat: a workspace for finishing tasks

- **Parallel subtasks:** the main agent can delegate to multiple subagents. Each task keeps its own context, attempts, and tool output, with queueing, progress inspection, and follow-up runs through `resume_id`
- **Visible execution:** press `Ctrl+G` or enter `/tasks` to inspect task status, individual attempts, and large outputs without flooding the main conversation with tool traffic
- **Long sessions that remain usable:** complete history is appended and paged through an index; active context can be compacted automatically or manually, and reopening a session does not load the entire transcript into memory
- **Reversible file changes:** automatic checkpoints let you restore file state by user message with `/rewind`
- **Controlled automation:** approvals, sandboxing, Plan Mode, cancellation, and mid-run steering keep explicit human control points

Built-in file, shell, code-index, web-search, and page-fetching tools cover project research through verification. Image understanding and generation through configured models are also available. The task center is currently TUI-only and scoped to the current parent session; the in-memory waiting queue is not restored across processes.

### One core, multiple ways to work

| Scenario | NekoCode capability |
|---|---|
| Daily terminal development | Bubble Tea TUI, streaming output, history paging, and a task center |
| Work away from your computer | Telegram, Feishu, QQ, and WeCom connectors |
| Editor integration | Stable Agent Client Protocol (ACP) v1 |
| Scripts and custom products | Headless NDJSON, the Runtime Go API, and HTTP/SSE |
| Custom tools and workflows | Skills, plugins, MCP, hooks, and a tested Claude Code-compatible subset |

### No single-vendor model or data lock-in

Connect Anthropic, DeepSeek, Kimi, GLM, and other services through the implemented OpenAI or Anthropic protocol features, then switch with `/model`. Sessions, checkpoints, and run records stay in the documented local `~/.nekocode` directory for backup, auditing, and customization.

The TUI ships as one executable; Linux builds require a compatible glibc environment. The `bot → runtime → interaction` architecture lets different applications share the same agent core.

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
  "active": "deepseek",
  "models": [
    {
      "name": "deepseek",
      "provider": "deepseek",
      "api_key": "sk-put-your-key-here",
      "model": "deepseek-v4-flash",
      "base_url": "https://api.deepseek.com/v1",
      "protocol": "openai"
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

ACP mode communicates over stdio and does not start the TUI. Session configuration is scoped to the current ACP connection and never overwrites global user configuration. Client-supplied stdio MCP processes are disabled by default; use `nekocode-tui --acp --allow-client-mcp` only when the client and its workspace configuration are fully trusted. See [ACP.md](ACP.md) for capabilities and limits.

### Use as a Stream JSON subprocess

```bash
nekocode-tui -p "Explain this project" --output-format stream-json
nekocode-tui --headless
```

Supports NDJSON turns, tool events, approvals, questions, and cancellation. This is the
NekoCode-owned `nekocode-headless/2` protocol with session management, model selection,
checkpoint rewind, subagent output, steering, and graceful shutdown.
See [STREAM_JSON.md](STREAM_JSON.md) for the supported contract (Chinese).

## Learn More

- Detailed usage and configuration guide: [User Guide](USER_GUIDE.md)
- The desktop (GUI) version is not yet shipped with releases; it is still under active development

NekoCode is still in testing and has not been validated for large production deployments.

The developer documents cover tool design, prefix caching, context compaction, and governance policies. Detailed design documents are currently maintained in Chinese.

## For Developers

- [ACP.md](ACP.md): ACP v1 methods, capability negotiation, and NekoCode implementation status (Chinese)
- [ARCHITECTURE.md](ARCHITECTURE.md): overall architecture, agent loop, tool system, and technical details
- [DATA_DIRECTORY.md](DATA_DIRECTORY.md): on-disk layout under `~/.nekocode`, data flow, and what is safe to delete (Chinese)
- [SUBAGENT_EVOLUTION_PLAN.md](SUBAGENT_EVOLUTION_PLAN.md): implemented task baseline, later design stages, and risk gates (Chinese)
- [IMAGES.md](IMAGES.md): image input/output support and model consumption paths (Chinese)
- [Headless guide](../headless/README.md): NDJSON integration, lifecycle events, and control protocol (Chinese)
- [CI_CD.md](CI_CD.md): CI, release gates, artifact verification, and failure handling (Chinese)
- [RUNTIME_APP_GUIDE.md](RUNTIME_APP_GUIDE.md): build your own AI application on top of the runtime
- [RUNTIME_HTTP_API.md](RUNTIME_HTTP_API.md): Runtime HTTP/SSE protocol
- [CHANGELOG.md](CHANGELOG.md): release history (Chinese)

## Contributing

- [CONTRIBUTING.en.md](CONTRIBUTING.en.md): contribution guide, coding standards, and test requirements
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md): community conduct expectations
- [GOVERNANCE.md](GOVERNANCE.md): maintainer and release responsibilities
- [SECURITY.md](SECURITY.md): how to report security vulnerabilities
- [SUPPORT.md](SUPPORT.md): supported environments and help channels

## License

NekoCode is available under the [MIT License](../LICENSE).

## Star History

<a href="https://www.star-history.com/?repos=lznauy%2FNekoCode&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
   <img alt="NekoCode Star History Chart" src="https://raw.githubusercontent.com/lznauy/NekoCode/star-history/assets/star-history/star-history-light.svg" />
 </picture>
</a>
