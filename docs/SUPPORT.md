# Support

NekoCode is maintained as an early-stage open source project. The latest tagged
release and the `master` branch receive bug fixes. Older releases may be useful
for comparison, but they do not receive routine support.

## Before asking for help

Check [README.md](../README.md), [USER_GUIDE.md](USER_GUIDE.md), and
existing issues. Include the NekoCode version, operating system, provider and
model, relevant configuration with secrets removed, and a minimal reproduction.

Use a GitHub issue for reproducible bugs and feature requests. General usage
questions may also be opened as issues while the project has no separate
discussion forum. Response times are best effort; the project has no paid SLA.

Do not report vulnerabilities in public issues. Follow [SECURITY.md](SECURITY.md)
for private reporting.

## Supported environments

Release binaries target Linux and macOS on amd64 and arm64. Linux binaries need
a compatible glibc environment. Windows is supported through WSL. The desktop
GUI remains under development and is not part of the supported release
artifacts.

Provider compatibility is limited to the protocol features implemented and
tested by NekoCode. An OpenAI-compatible or Anthropic-compatible label does not
guarantee that every vendor extension, reasoning mode, or tool-call behavior is
supported.
