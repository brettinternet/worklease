# Container deployment

The release workflow publishes the same statically linked Linux binary from the
release archives as `ghcr.io/brettinternet/worklease:vVERSION` for Linux amd64
and arm64.

The runtime image is `scratch`: it contains only `/worklease`, runs as UID/GID
`65532:65532`, and adds no container-only Worklease behavior. Pass the same CLI
commands and server configuration used by a native installation.

Version tags are immutable release identities. Publication fails when
`vVERSION` already exists; a later release must use a new version and cannot
replace an earlier version tag. Worklease does not publish `latest` or another
moving container tag. Confirm an image before deployment:

```sh
docker run --rm ghcr.io/brettinternet/worklease:v1.2.0 version
```

## Persistent hosted home

Mount exactly one writable, owner-private directory for the complete hosted
home. It must be owned by container UID/GID 65532 and must survive every restart
and image replacement:

```sh
sudo install -d -m 0700 -o 65532 -g 65532 /srv/worklease/home
sudo install -d -m 0700 -o 65532 -g 65532 /srv/worklease/bootstrap
sudo install -d -m 0700 -o 65532 -g 65532 /etc/worklease
```

The complete home—not only `worklease.db`—is the durability boundary:

| State | Includes |
| --- | --- |
| SQLite | `worklease.db`, WAL, and SHM files |
| Process | `hosted`, `hosted.lock`, and `hosted.ready` |
| Authority | Authority and restore identities, claims, and operation replay |
| Recovery | Recovery state, installations, invites, and reopening records |

Keep current and future authority files on one mount. Never mount individual
files or split the home across volumes.

SQLite WAL requires a local, single-host filesystem. Network filesystems are
unsupported.

Run one writable container against a hosted home, never multiple replicas or two
writable copies. Upgrades are stop-before-start: stop the old container
completely, retain the home, then start the new image.

The hosted lock rejects a second cooperating writer but cannot make independent
writable copies safe.

## Configuration, TLS, and initialization

Use guided setup instead of hand-writing YAML or handling authority metadata.
The setup directory is writable only for initialization so Worklease can create
its owner-private configuration, generated pinned certificate, private key, and
bootstrap artifact. Keep every mounted directory outside source checkouts.

```sh
docker run --rm \
  --env XDG_STATE_HOME=/var/lib \
  --env XDG_CONFIG_HOME=/run \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease/server \
  --mount type=bind,src=/etc/worklease,dst=/run/worklease \
  --mount type=bind,src=/srv/worklease/bootstrap,dst=/run/bootstrap \
  ghcr.io/brettinternet/worklease:v1.2.0 \
  server init \
  --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/bootstrap/admin.invite \
  --listen 0.0.0.0:7443 \
  --endpoint https://worklease.example.com:7443 \
  --transport tls \
  --admitted-prefix coordination: \
  --confirm-non-loopback
```

Transfer `/srv/worklease/bootstrap/admin.invite` through an authenticated
secret channel to the administrator. Enroll with
`worklease enroll --invite-file FILE`.

The artifact carries the endpoint, authority identity, and generated certificate pin;
do not parse or copy those fields manually. The administrator then issues a write
artifact with `worklease invite issue --role write --invite-file FILE --label client`
for the separate client.

Follow the complete claim, doctor, observation, heartbeat, and release journey in
the [remote authority quickstart](remote-claim-authority.md#two-machine-quickstart).

Run the authority with only the hosted home writable and generated setup mounted
read-only:

```sh
docker run --detach --name worklease --restart unless-stopped \
  --publish 7443:7443 \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease/server \
  --mount type=bind,src=/etc/worklease,dst=/run/worklease,readonly \
  ghcr.io/brettinternet/worklease:v1.2.0 \
  serve --server-config /run/worklease/server.yaml
```

Do not bake configuration, TLS private keys, invite secrets, installation
credentials, or backup credentials into an image.

Import one-time artifacts into the intended secret manager and remove them
according to that system's policy. A TLS-terminating proxy is optional; if used,
follow the server's trusted-edge requirements rather than passing identity
headers as authorization.

## Backup and upgrade

A restart or stop-before-start image upgrade reuses the mounted home unchanged.
This preserves the authority identity, restore identity, claims, replay, and
authentication state.

Never copy a live database file independently of its WAL state. Online
replication to object storage is disaster recovery, not high availability, and
can lose acknowledged writes after the last upload.

### Online backup with Litestream

This recipe replicates the authority to S3-compatible storage with
Litestream 0.5. `mise run backup-drill` runs it end to end against SeaweedFS:
replicate, lose the home, restore, and verify recovery mode and the cutoff.

Keep Litestream state and credentials outside the authority home:

```sh
sudo install -d -m 0700 -o 65532 -g 65532 /srv/worklease/litestream
sudo install -d -m 0700 -o 65532 -g 65532 /srv/worklease/restore
sudo install -d -m 0700 -o 65532 -g 65532 /etc/litestream
```

`/etc/litestream/litestream.yml`, mode 0600 and owned by 65532:

```yaml
socket:
  enabled: true
  path: /var/lib/litestream/litestream.sock
dbs:
  - path: /var/lib/worklease/server/worklease.db
    meta-path: /var/lib/litestream/worklease.db-litestream
    replica:
      type: s3
      endpoint: https://s3.example.com
      bucket: worklease-backup
      path: authority
      force-path-style: true
```

`/etc/litestream/s3.env`, mode 0600, supplies
`LITESTREAM_ACCESS_KEY_ID` and `LITESTREAM_SECRET_ACCESS_KEY`. Run the
replicator beside the authority on the same host:

```sh
docker run --detach --name worklease-backup --restart unless-stopped \
  --user 65532:65532 \
  --env-file /etc/litestream/s3.env \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease/server \
  --mount type=bind,src=/srv/worklease/litestream,dst=/var/lib/litestream \
  --mount type=bind,src=/etc/litestream/litestream.yml,dst=/etc/litestream.yml,readonly \
  litestream/litestream:0.5.17 replicate
```

Litestream writes to the database: it adds `_litestream_seq` and
`_litestream_lock` tables and coordinates WAL checkpoints. Worklease accepts
both. Stopping Litestream does not upload pending changes; confirm an upload
with `docker exec worklease-backup litestream sync -wait -socket
/var/lib/litestream/litestream.sock /var/lib/worklease/server/worklease.db`.

### Restore

Stop the old authority and establish its cessation time first. Restore the
replica into the staging directory; `server restore` accepts only an
owner-private file:

```sh
docker run --rm --user 65532:65532 \
  --env-file /etc/litestream/s3.env \
  --mount type=bind,src=/srv/worklease/restore,dst=/restore \
  --mount type=bind,src=/etc/litestream/litestream.yml,dst=/etc/litestream.yml,readonly \
  litestream/litestream:0.5.17 \
  restore -integrity-check full -o /restore/worklease.db /var/lib/worklease/server/worklease.db
sudo chmod 0600 /srv/worklease/restore/worklease.db
```

If the home was lost, recreate it with the `server init` command above. Then
install the backup and reissue the bootstrap invite as an enrollment artifact:

```sh
docker run --rm \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease/server \
  --mount type=bind,src=/srv/worklease/restore,dst=/restore,readonly \
  --mount type=bind,src=/srv/worklease/bootstrap,dst=/run/bootstrap \
  ghcr.io/brettinternet/worklease:v1.2.0 \
  server restore --home /var/lib/worklease/server --from /restore/worklease.db \
  --loss-interval-end 2026-09-14T00:05:00Z \
  --bootstrap-invite-file /run/bootstrap/restore.secret
docker run --rm \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease/server \
  --mount type=bind,src=/etc/worklease,dst=/run/worklease,readonly \
  --mount type=bind,src=/srv/worklease/bootstrap,dst=/run/bootstrap \
  ghcr.io/brettinternet/worklease:v1.2.0 \
  server bootstrap-reissue --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/bootstrap/admin.invite
```

`server restore` creates a new incarnation in recovery mode. Its selected
durable cutoff defaults to the backup's last committed write, which restore
output reports. Litestream's file timestamps record uploads, not commits, so do
not use them as the cutoff. Start the authority, enroll with the reissued
invite, and follow the
[remote authority recovery model](remote-claim-authority.md#restore-recovery-and-reopening)
to reopen.

| Need | Coverage |
| --- | --- |
| Scheduling | Litestream replicates continuously. |
| Retention | Litestream replica retention and compaction. |
| Verification | `restore -integrity-check full`, restore schema checks, and periodic `mise run backup-drill`. |
| Cutoff | `server restore` records the backup's last committed write. |
