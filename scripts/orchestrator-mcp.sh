#!/usr/bin/env bash
# Wrapper script to run the Orchestrator MCP server as root.
# VM operations require root; this keeps `sudo` out of the client's MCP config.
#
# Install: ln -s "$PWD/scripts/orchestrator-mcp.sh" /usr/local/bin/orchestrator-mcp
# Usage in ~/.claude/mcp.json: { "command": "orchestrator-mcp" }
set -euo pipefail

# Resolve the real directory this script lives in, even through symlinks, then
# the project root is one level up (scripts/ -> repo root) and the binary lives
# in bin/orchestrator.
SCRIPT="$(readlink -f -- "${BASH_SOURCE[0]}")"
DIR="$(dirname -- "$SCRIPT")"
ROOT="$(dirname -- "$DIR")"

exec sudo -E "$ROOT/bin/orchestrator" mcp "$@"
