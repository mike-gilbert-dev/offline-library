#!/bin/sh
# macOS: double-click to start the Offline Library. Picks the launcher that
# matches this Mac's processor (Apple Silicon or Intel).
cd "$(dirname "$0")" || exit 1
if [ "$(uname -m)" = "arm64" ]; then
  exec ./start-mac-apple-silicon "$@"
else
  exec ./start-mac-intel "$@"
fi
