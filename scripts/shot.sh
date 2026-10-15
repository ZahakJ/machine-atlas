#!/bin/sh
# Runs scripts/shot.py under a lock; concurrent calls wait.
# Usage: scripts/shot.sh URL OUT.png [options]; see scripts/shot.py --help.
set -eu
here=$(dirname "$0")
exec flock /tmp/machine-atlas-browser.lock python3 "$here/shot.py" "$@"
