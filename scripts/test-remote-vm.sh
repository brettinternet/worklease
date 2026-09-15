#!/bin/sh
set -eu

instance=worklease-remote
host=lima-worklease-remote
evidence=${1:-dist/remote-acceptance/vm-$(date -u +%Y%m%dT%H%M%SZ)}

if limactl list --format '{{.Name}}' | grep -Fxq "$instance"; then
  limactl start --tty=false "$instance"
else
  limactl start --tty=false --name "$instance" --cpus 2 --memory 2 template:default
fi

ssh_config=$(limactl list --format '{{.SSHConfigFile}}' "$instance")
test -f "$ssh_config"

go run ./cmd/worklease-remote-smoke \
  --binary ./bin/worklease \
  --remote-host "$host" \
  --ssh-config "$ssh_config" \
  --evidence "$evidence"
