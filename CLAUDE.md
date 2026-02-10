# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build the binary
go build -o irctoslack .

# Run directly (requires config.yaml in working directory)
go run irc2slack.go

# Run in the background (logs to irc2slack.log)
./irctoslack -d

# Generate a sample config.yaml
./irctoslack --generate-config > config.yaml

# Cross-compile (CI builds linux/amd64 and linux/arm64)
GOOS=linux GOARCH=amd64 go build -o irctoslack-linux-amd64 .
```

Running without a `config.yaml` prints a help screen. There are no tests and no linter configured.

## Architecture

This is a single-file Go application (`irc2slack.go`) that acts as a bidirectional IRC-to-Slack bridge. The binary name is `irctoslack` but the source file is `irc2slack.go`.

**IRC → Slack:** A persistent TCP connection to the IRC server reads messages in a loop (`manageIRCConnection` → `handleMessage`). PRIVMSG, JOIN, PART, and ACTION events are parsed and forwarded to Slack via an incoming webhook (`postToSlack`). The connection auto-reconnects on failure.

**Slack → IRC:** An HTTP server listens for Slack Event API webhooks on `/webhook` (`createWebhookHandler`). Incoming messages are sent to IRC as PRIVMSG. It handles the Slack `url_verification` challenge. Message filtering (`shouldProcessMessage`) skips bot messages and ignored users.

**User resolution:** Slack user IDs (e.g., `<@U1234>`) are resolved to display names via the Slack API (`getUserDisplayName`), cached in-memory for 1 hour with a RWMutex-protected map. `translateMentions` replaces all `<@UXXXXX>` patterns in message text.

**Configuration:** Loaded from `config.yaml` (YAML) at startup via `loadConfig`. Contains IRC server/channel/nick, Slack webhook URL, listen address, API token, and ignore lists. The config file is gitignored. `--generate-config` prints an annotated sample config.

**CLI flags:** Parsed in `main()` with `flag`. `--generate-config` prints sample config and exits. `-d` re-execs the binary with stdout/stderr redirected to `irc2slack.log` via `os/exec`, then the parent exits. Missing `config.yaml` prints a help screen and exits with code 1.

**Concurrency:** IRC writes are protected by a mutex on `IRCConnection`. The IRC reader loop and HTTP server run in separate goroutines. A channel synchronizes initial connection readiness before starting the HTTP server.

**Releases:** CI builds on push to main and creates a GitHub release with CalVer tags (`YYYY.MM.DD`, incrementing `.N` suffix for same-day releases). Binaries for linux/amd64 and linux/arm64 are attached as release assets.
