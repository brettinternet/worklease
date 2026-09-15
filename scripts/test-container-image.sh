#!/bin/sh
set -eu

usage() {
  echo "usage: $0 IMAGE VERSION BINARY [UPGRADE_IMAGE]" >&2
  exit 2
}

[ "$#" -ge 3 ] && [ "$#" -le 4 ] || usage
image=$1
version=$2
binary=$3
upgrade_image=${4:-$image}

[ -x "$binary" ] || { echo "released binary is not executable: $binary" >&2; exit 1; }
command -v docker >/dev/null
command -v curl >/dev/null
command -v jq >/dev/null
command -v openssl >/dev/null

smoke_root=$(mktemp -d "${TMPDIR:-/tmp}/worklease-container-smoke.XXXXXX")
case "$smoke_root" in
  "${TMPDIR:-/tmp}"/worklease-container-smoke.*) ;;
  *) echo "unsafe smoke path: $smoke_root" >&2; exit 1 ;;
esac
touch "$smoke_root/.worklease-container-smoke-owner"
home=$smoke_root/authority
secrets=$smoke_root/secrets
client_home=$smoke_root/client-home
client_config=$smoke_root/client-config
mkdir -m 700 "$home" "$secrets" "$client_home" "$client_config"

server_name=worklease-container-smoke-$$
export_name=worklease-container-export-$$
cleanup() {
  docker container stop "$server_name" >/dev/null 2>&1 || true
  docker container rm "$server_name" >/dev/null 2>&1 || true
  docker container rm "$export_name" >/dev/null 2>&1 || true
  echo "container smoke evidence retained at $smoke_root" >&2
}
trap cleanup EXIT HUP INT TERM

configured_user=$(docker image inspect "$image" --format '{{.Config.User}}')
[ "$configured_user" = "65532:65532" ] || {
  echo "image user is $configured_user, want 65532:65532" >&2
  exit 1
}

image_version=$(docker run --rm "$image" --json version | jq -r '.version')
[ "$image_version" = "$version" ] || {
  echo "image version is $image_version, want $version" >&2
  exit 1
}

docker create --name "$export_name" "$image" >/dev/null
filesystem=$(docker export "$export_name" | tar -tf - | sed 's#^\./##' \
  | grep -v '/$' \
  | grep -Ev '^(\.dockerenv|dev/console|etc/(hostname|hosts|mtab|resolv.conf))$' || true)
[ "$filesystem" = "worklease" ] || {
  echo "unexpected runtime filesystem:" >&2
  printf '%s\n' "$filesystem" >&2
  exit 1
}
docker container rm "$export_name" >/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=127.0.0.1' -addext 'subjectAltName=IP:127.0.0.1' \
  -keyout "$secrets/tls.key" -out "$secrets/tls.crt" >/dev/null 2>&1
chmod 600 "$secrets/tls.key" "$secrets/tls.crt"
cat >"$secrets/server.yaml" <<EOF
home: /var/lib/worklease
listen: 0.0.0.0:7443
tlsCert: /run/worklease/tls.crt
tlsKey: /run/worklease/tls.key
admittedPrefixes:
  - 'coordination:'
maxTTL: 1h
maxHold: 24h
shutdownTimeout: 5s
healthRate: 100
metadataRate: 100
enrollmentRate: 100
EOF
chmod 600 "$secrets/server.yaml"

run_image() {
  selected_image=$1
  docker run -d --name "$server_name" \
    --user "$(id -u):$(id -g)" \
    --network host \
    --mount "type=bind,source=$home,target=/var/lib/worklease" \
    --mount "type=bind,source=$secrets,target=/run/worklease,readonly" \
    "$selected_image" serve --server-config /run/worklease/server.yaml >/dev/null
}

wait_healthy() {
  attempt=0
  until curl --fail --silent --show-error --header 'Accept: application/json' \
    --cacert "$secrets/tls.crt" "$endpoint/healthz" >/dev/null; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt 50 ] || {
      docker inspect "$server_name" --format '{{json .State}}' >&2
      docker logs "$server_name" >&2
      exit 1
    }
    sleep 0.1
  done
}

echo "initializing hosted home"
if init_output=$(docker run --rm \
  --user "$(id -u):$(id -g)" \
  --mount "type=bind,source=$home,target=/var/lib/worklease" \
  --mount "type=bind,source=$secrets,target=/run/worklease" \
  "$image" --json hosted init --home /var/lib/worklease \
  --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/worklease/bootstrap.invite); then
  :
else
  echo "hosted init failed: $init_output" >&2
  exit 1
fi
authority_id=$(printf '%s' "$init_output" | jq -er '.authorityId') || {
  echo "hosted init result has no authorityId" >&2
  exit 1
}

run_image "$image"
endpoint=https://127.0.0.1:7443
wait_healthy

export WORKLEASE_HOME="$client_home"
export XDG_CONFIG_HOME="$client_config"
export SSL_CERT_FILE="$secrets/tls.crt"
export WORKLEASE_AGENT_ID=container-smoke
export WORKLEASE_SESSION_ID=container-smoke
echo "enrolling client and acquiring durable claim"
"$binary" profile add team --endpoint "$endpoint" --authority-id "$authority_id" >/dev/null
"$binary" enroll --profile team --invite-file "$secrets/bootstrap.invite" --label container-smoke >/dev/null
acquire_output=$("$binary" --json --profile team acquire --resource coordination:container-smoke --session container-smoke --ttl 10m)
claim_id=$(printf '%s' "$acquire_output" | jq -er '.claimId') || {
  echo "acquire result has no claimId" >&2
  exit 1
}

echo "restarting container while retaining hosted home"
docker restart --time 10 "$server_name" >/dev/null
wait_healthy
restart_output=$("$binary" --json --profile team heartbeat --session container-smoke --ttl 10m)
restart_claim=$(printf '%s' "$restart_output" | jq -er '.receipt.claimId') || {
  echo "post-restart heartbeat result has no receipt claimId" >&2
  exit 1
}
[ "$restart_claim" = "$claim_id" ] || {
  echo "restart returned claim $restart_claim, want $claim_id" >&2
  exit 1
}

# Stop the old process before starting the replacement against the same home.
echo "replacing stopped server while retaining hosted home"
docker stop --time 10 "$server_name" >/dev/null
docker container rm "$server_name" >/dev/null
run_image "$upgrade_image"
wait_healthy

echo "verifying durable authority, authentication, and claim state"
heartbeat_output=$("$binary" --json --profile team heartbeat --session container-smoke --ttl 10m)
heartbeat_claim=$(printf '%s' "$heartbeat_output" | jq -er '.receipt.claimId') || {
  echo "post-upgrade heartbeat result has no receipt claimId" >&2
  exit 1
}
[ "$heartbeat_claim" = "$claim_id" ] || {
  echo "replacement returned claim $heartbeat_claim, want $claim_id" >&2
  exit 1
}
"$binary" --profile team release --session container-smoke --reason container-smoke-complete >/dev/null

printf 'validated image=%s version=%s authority=%s claim=%s\n' "$image" "$version" "$authority_id" "$claim_id"
