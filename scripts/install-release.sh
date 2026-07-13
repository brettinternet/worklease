#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PYTHON=${WORKLEASE_PYTHON:-python3}
exec "$PYTHON" "$SCRIPT_DIR/release_installer.py" "$@"
