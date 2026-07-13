#!/bin/sh
set -eu

if [ "$#" -gt 0 ]; then
  case "$1" in
    VERSION=*)
      VERSION=${1#VERSION=}
      export VERSION
      shift
      ;;
  esac
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PYTHON=${WORKLEASE_PYTHON:-python3}
exec "$PYTHON" "$SCRIPT_DIR/release_installer.py" "$@"
