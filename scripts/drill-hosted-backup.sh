#!/bin/sh
# Drill the documented Litestream backup recipe for a hosted authority:
# replicate to S3-compatible storage, lose the authority home, restore through
# `worklease server restore`, and verify recovery mode and the recorded cutoff.
# Docker must be able to bind-mount TMPDIR (for example, under $HOME on Colima).
set -eu

usage() {
  echo "usage: $0 IMAGE BINARY" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
image=$1
binary=$2
litestream_image=${LITESTREAM_IMAGE:-litestream/litestream:0.5.17}
s3_image=${S3_IMAGE:-chrislusf/seaweedfs:4.48}
port=${DRILL_PORT:-17443}

[ -x "$binary" ] || { echo "client binary is not executable: $binary" >&2; exit 1; }
command -v docker >/dev/null
command -v curl >/dev/null
command -v jq >/dev/null
command -v openssl >/dev/null

drill_root=$(mktemp -d "${TMPDIR:-/tmp}/worklease-backup-drill.XXXXXX")
case "$drill_root" in
  "${TMPDIR:-/tmp}"/worklease-backup-drill.*) ;;
  *) echo "unsafe drill path: $drill_root" >&2; exit 1 ;;
esac
touch "$drill_root/.worklease-backup-drill-owner"
secrets=$drill_root/secrets
backup_secrets=$drill_root/backup-secrets
evidence=$drill_root/evidence
client_home=$drill_root/client-home
client_config=$drill_root/client-config
mkdir -m 700 "$secrets" "$backup_secrets" "$evidence" "$client_home" "$client_config"

id=$$
network=worklease-drill-$id
home_volume=worklease-drill-home-$id
restored_volume=worklease-drill-restored-$id
meta_volume=worklease-drill-litestream-$id
staging_volume=worklease-drill-staging-$id
s3_name=worklease-drill-s3-$id
replicator_name=worklease-drill-litestream-$id
server_name=worklease-drill-server-$id
user="$(id -u):$(id -g)"

cleanup() {
  for name in "$server_name" "$replicator_name" "$s3_name"; do
    docker container rm --force "$name" >/dev/null 2>&1 || true
  done
  for volume in "$home_volume" "$restored_volume" "$meta_volume" "$staging_volume"; do
    docker volume rm "$volume" >/dev/null 2>&1 || true
  done
  docker network rm "$network" >/dev/null 2>&1 || true
  echo "backup drill evidence retained at $evidence" >&2
}
trap cleanup EXIT HUP INT TERM

fail() {
  echo "$*" >&2
  exit 1
}

docker network create "$network" >/dev/null
for volume in "$home_volume" "$restored_volume" "$meta_volume" "$staging_volume"; do
  docker volume create "$volume" >/dev/null
  docker run --rm --user 0 --entrypoint /bin/sh \
    --mount "type=volume,source=$volume,target=/v" "$litestream_image" \
    -c "chown $user /v && chmod 700 /v"
done

openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=127.0.0.1' -addext 'subjectAltName=IP:127.0.0.1' \
  -keyout "$secrets/tls.key" -out "$secrets/tls.crt" >/dev/null 2>&1
cat >"$secrets/server.yaml" <<EOF
home: /var/lib/worklease
listen: 0.0.0.0:$port
advertisedEndpoint: https://127.0.0.1:$port
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
chmod 600 "$secrets/tls.key" "$secrets/tls.crt" "$secrets/server.yaml"

# Replication credentials and Litestream configuration live outside the
# authority home and are mounted read-only.
access_key=drill$(openssl rand -hex 8)
secret_key=$(openssl rand -hex 24)
cat >"$backup_secrets/s3.json" <<EOF
{"identities":[{"name":"worklease-backup","credentials":[{"accessKey":"$access_key","secretKey":"$secret_key"}],"actions":["Admin","Read","Write","List","Tagging"]}]}
EOF
cat >"$backup_secrets/litestream.env" <<EOF
LITESTREAM_ACCESS_KEY_ID=$access_key
LITESTREAM_SECRET_ACCESS_KEY=$secret_key
EOF
cat >"$backup_secrets/litestream.yml" <<EOF
logging:
  stderr: true
socket:
  enabled: true
  path: /var/lib/litestream/litestream.sock
dbs:
  - path: /var/lib/worklease/worklease.db
    meta-path: /var/lib/litestream/worklease.db-litestream
    replica:
      type: s3
      endpoint: http://$s3_name:8333
      region: us-east-1
      bucket: worklease-backup
      path: authority
      force-path-style: true
EOF
chmod 600 "$backup_secrets"/*
chmod 644 "$backup_secrets/s3.json"

worklease() {
  selected_home=$1
  shift
  docker run --rm --user "$user" \
    --mount "type=volume,source=$selected_home,target=/var/lib/worklease" \
    --mount "type=bind,source=$secrets,target=/run/worklease" \
    --mount "type=volume,source=$staging_volume,target=/restore,readonly" \
    "$image" "$@"
}

litestream() {
  docker run --rm --user "$user" --network "$network" \
    --env-file "$backup_secrets/litestream.env" \
    --mount "type=bind,source=$backup_secrets/litestream.yml,target=/etc/litestream.yml,readonly" \
    "$@"
}

serve() {
  docker run -d --name "$server_name" --user "$user" \
    --publish "127.0.0.1:$port:$port" \
    --mount "type=volume,source=$1,target=/var/lib/worklease" \
    --mount "type=bind,source=$secrets,target=/run/worklease,readonly" \
    "$image" serve --server-config /run/worklease/server.yaml >/dev/null
  attempt=0
  until curl --fail --silent --header 'Accept: application/json' --cacert "$secrets/tls.crt" "https://127.0.0.1:$port/healthz" >/dev/null; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt 100 ] || { docker logs "$server_name" >&2; fail "authority did not become healthy"; }
    sleep 0.1
  done
}

stop_server() {
  docker stop --time 10 "$server_name" >/dev/null
  docker container rm "$server_name" >/dev/null
}

replica_sync() {
  docker exec "$replicator_name" litestream sync -wait -timeout 60 -json \
    -socket /var/lib/litestream/litestream.sock /var/lib/worklease/worklease.db >"$1"
}

utc_now() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

echo "starting S3-compatible storage"
docker run -d --name "$s3_name" --network "$network" \
  --mount "type=bind,source=$backup_secrets/s3.json,target=/etc/seaweedfs/s3.json,readonly" \
  "$s3_image" mini -dir=/data -s3.config=/etc/seaweedfs/s3.json -bucket=worklease-backup >/dev/null
attempt=0
until docker exec "$s3_name" wget -q -O /dev/null http://127.0.0.1:8333/healthz >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 120 ] || { docker logs "$s3_name" >&2; fail "S3 storage did not become ready"; }
  sleep 0.5
done

echo "initializing hosted authority"
worklease "$home_volume" --json server init --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/worklease/bootstrap.invite >"$evidence/server-init.json"
authority_id=$(jq -er '.authorityId' "$evidence/server-init.json")

echo "starting authority and replicator"
serve "$home_volume"
docker run -d --name "$replicator_name" --user "$user" --network "$network" \
  --env-file "$backup_secrets/litestream.env" \
  --mount "type=bind,source=$backup_secrets/litestream.yml,target=/etc/litestream.yml,readonly" \
  --mount "type=volume,source=$home_volume,target=/var/lib/worklease" \
  --mount "type=volume,source=$meta_volume,target=/var/lib/litestream" \
  "$litestream_image" replicate >/dev/null

# Snapshot first so the claim below replicates as incremental WAL changes.
replica_sync "$evidence/litestream-sync-snapshot.json"

export WORKLEASE_HOME="$client_home"
export XDG_CONFIG_HOME="$client_config"
export SSL_CERT_FILE="$secrets/tls.crt"
export WORKLEASE_AGENT_ID=backup-drill
export WORKLEASE_SESSION_ID=backup-drill
"$binary" enroll --profile drill --invite-file "$secrets/bootstrap.invite" --label backup-drill >/dev/null
"$binary" --json --profile drill acquire --resource coordination:drill-replicated \
  --session backup-drill --ttl 10m >"$evidence/acquire-replicated.json"
replicated_claim=$(jq -er '.claimId' "$evidence/acquire-replicated.json")

# Stopping Litestream does not flush pending changes; wait for the upload.
replica_sync "$evidence/litestream-sync-claim.json"
docker stop --time 30 "$replicator_name" >"$evidence/litestream-stop.txt"
docker logs "$replicator_name" >"$evidence/litestream-replicate.log" 2>&1
docker container rm "$replicator_name" >/dev/null

echo "writing an unreplicated tail and stopping the authority"
"$binary" --json --profile drill acquire --resource coordination:drill-lost \
  --session backup-drill-lost --ttl 10m >"$evidence/acquire-lost.json"
stop_server
# Round cessation up to a whole second; a later loss-interval end is conservative.
sleep 1
cessation=$(utc_now)
docker run --rm --mount "type=volume,source=$home_volume,target=/v,readonly" \
  --entrypoint /bin/ls "$litestream_image" -a /v >"$evidence/home-contents.txt"
for unexpected in worklease.db-litestream .worklease.db-litestream; do
  if grep -qx "$unexpected" "$evidence/home-contents.txt"; then
    fail "replicator state was written into the authority home"
  fi
done

echo "simulating loss of the authority home"
docker volume rm "$home_volume" >/dev/null

echo "restoring the replica"
litestream --mount "type=volume,source=$staging_volume,target=/restore" "$litestream_image" \
  restore -json -integrity-check full -o /restore/worklease.db /var/lib/worklease/worklease.db \
  >"$evidence/litestream-restore.json"
restored_txid=$(jq -er '.txid' "$evidence/litestream-restore.json")
litestream "$litestream_image" ltx /var/lib/worklease/worklease.db >"$evidence/litestream-ltx.txt"
# `server restore` accepts only an owner-private backup file.
docker run --rm --user "$user" --entrypoint /bin/chmod \
  --mount "type=volume,source=$staging_volume,target=/restore" "$litestream_image" \
  600 /restore/worklease.db

echo "recreating the hosted home and restoring into recovery mode"
worklease "$restored_volume" --json server init --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/worklease/replacement.invite >"$evidence/replacement-init.json"
# The cutoff defaults to the backup's last committed authority write.
worklease "$restored_volume" --json server restore --home /var/lib/worklease \
  --from /restore/worklease.db --loss-interval-end "$cessation" \
  --bootstrap-invite-file /run/worklease/restore.invite >"$evidence/server-restore.json"
restored_authority=$(jq -er '.authorityId' "$evidence/server-restore.json")
[ "$restored_authority" = "$authority_id" ] || fail "restored authority $restored_authority, want $authority_id"
cutoff=$(jq -er '.selectedDurableCutoff' "$evidence/server-restore.json")

# Restore writes a bare bootstrap secret; reissue it as an enrollable artifact.
worklease "$restored_volume" --json server bootstrap-reissue --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/worklease/restored-admin.invite >"$evidence/bootstrap-reissue.json"

serve "$restored_volume"
"$binary" enroll --profile restored --invite-file "$secrets/restored-admin.invite" --label backup-drill-restored >/dev/null
"$binary" --json --profile restored recovery status >"$evidence/recovery-status.json"
"$binary" --json --profile restored history --resource coordination:drill-replicated >"$evidence/history-replicated.json"
"$binary" --json --profile restored history --resource coordination:drill-lost >"$evidence/history-lost.json"
stop_server

jq -e --arg cutoff "$cutoff" '.recoveryMode == true and .selectedDurableCutoff == $cutoff' \
  "$evidence/recovery-status.json" >/dev/null \
  || fail "restored authority is not in recovery mode with cutoff $cutoff"
jq -e --arg claim "$replicated_claim" '[.epochs[] | select(.claimId == $claim and .endReason == "restored")] | length == 1' \
  "$evidence/history-replicated.json" >/dev/null || fail "replicated claim did not survive restore"
jq -e '.epochs == []' "$evidence/history-lost.json" >/dev/null || fail "unreplicated tail survived restore"

printf 'authority=%s replicated_claim=%s restored_txid=%s cutoff=%s cessation=%s\n' \
  "$authority_id" "$replicated_claim" "$restored_txid" "$cutoff" "$cessation" | tee "$evidence/summary.txt"
