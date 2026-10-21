#!/usr/bin/env bash
# Compatibility entrypoint for installations that followed the previous guide.
# This script is no longer tied to pwd, the Go project root or a local ZIP file.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$HERE/update-online-linux.sh" "$@"
