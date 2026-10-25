# Security Policy

## Supported Versions

NekoCode is under active development. Security fixes target the latest tagged
release and the `master` branch.

| Version | Supported |
| --- | --- |
| latest release (v*) | Yes |
| master | Yes |
| older releases | No |

## Reporting a Vulnerability

**Please do not open a public issue for security vulnerabilities.**

Send the report through one of these private channels:

- Email: [lznauyfine@gmail.com](mailto:lznauyfine@gmail.com)
- GitHub Private Vulnerability Reporting, when enabled: use **Report a
  vulnerability** on the [Security Advisories page](https://github.com/lznauy/NekoCode/security/advisories/new)

Please include:

- The affected version (or commit SHA)
- A description of the vulnerability and its impact
- Reproduction steps or a minimal proof of concept
- Any suggested mitigation (if known)

The maintainer will acknowledge a complete report within seven days. The
acknowledgement is not a promise that a fix will ship within that period.
Please coordinate public disclosure with the maintainer so users have time to
install a fixed release.

## Scope

Security-sensitive areas include, but are not limited to:

- Prompt injection resistance and tool permission enforcement
- Shell command execution boundaries (`bash` / nested execution)
- File read/write scoping (workspace roots, path traversal, symlinks)
- Credential handling and secret leakage in logs or error messages
- Outbound network calls (SSRF, untrusted destinations)
- The one-line install script (`scripts/install.sh`)
- Session data and local persistence (checkpoints, call logs, config)

## Security Practices

- CI runs `govulncheck` to detect known vulnerabilities reachable from Go code.
- Call logs omit prompts, request bodies, response text, credentials, and raw
  provider fingerprints. They retain token usage, endpoint origins, local
  prefix hashes, and a short hash of the provider fingerprint for diagnostics.
- Permission rules and sandboxing reduce the impact of dangerous tool calls.
  Shell commands are parsed before rule evaluation; indirect execution such as
  substitutions, dynamic command names, `eval`, `source`, shell `-c`, and shell
  heredoc code requires an explicit decision unless that exact command was
  previously remembered. Unparseable commands cannot be remembered. This is a
  safety boundary, not a complete defense against prompt injection.
- Release assets include checksums and a software bill of materials. The
  release workflow also records GitHub build provenance. The installer verifies
  the selected binary against `SHA256SUMS`, or vetted checksums bundled for
  v0.4.2, before replacing an existing installation.
- If you change security-sensitive code, please describe the threat model in
  your PR and add regression tests for the boundary you touched.
