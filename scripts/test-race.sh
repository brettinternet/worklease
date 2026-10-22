#!/bin/sh
set -eu

# Partition package identities, not test names: new packages automatically land
# in rest and new CLI tests automatically land in cli. Each runs exactly once.
packages=$(go list ./...)
cli="$(go list -m)/internal/cli"
case "${1:-all}" in
  all) ;;
  race-cli) packages=$(printf '%s\n' "$packages" | grep -Fx "$cli") ;;
  race-rest) packages=$(printf '%s\n' "$packages" | grep -Fvx "$cli") ;;
  *) echo "unknown race partition: $1" >&2; exit 2 ;;
esac
# Listing is also used to audit disjoint, exhaustive partition membership.
if [ "${2:-}" = --list ]; then
  printf '%s\n' "$packages"
  exit 0
fi
# Re-executed test processes are waited on and reaped. Preserve race detection
# and failure exits without the race runtime's unconditional exit sleep.
export GORACE="${GORACE:-} atexit_sleep_ms=0"
# Intentional splitting: go list emits whitespace-free package import paths.
# shellcheck disable=SC2086
go test -race -p 1 -parallel 2 $packages
