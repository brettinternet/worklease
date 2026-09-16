# Container deployment

The release workflow publishes the same statically linked Linux binary from the
release archives as `ghcr.io/brettinternet/worklease:vVERSION` for Linux amd64
and arm64. The runtime image is `scratch`: it contains only `/worklease`, runs as
UID/GID `65532:65532`, and adds no container-only Worklease behavior. Pass the
same CLI commands and server configuration used by a native installation.

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
unsupported. Run one writable container against a hosted home, never multiple
replicas or two writable copies. Upgrades are stop-before-start: stop the old
container completely, retain the home, then start the new image. The hosted lock
rejects a second cooperating writer but cannot make independent writable copies
safe.

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
  server init --guided \
  --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/bootstrap/admin.invite \
  --listen 0.0.0.0:7443 \
  --endpoint https://worklease.example.com:7443 \
  --transport tls \
  --admitted-prefix coordination: \
  --confirm-non-loopback
```

Transfer `/srv/worklease/bootstrap/admin.invite` through an authenticated
secret channel to the administrator and enroll with
`worklease enroll --invite-file FILE`. The artifact carries the endpoint,
authority identity, and generated certificate pin; do not parse or copy those
fields manually. The administrator then issues a write artifact with
`worklease invite issue --role write --invite-file FILE --label client` for the
separate client. Follow the complete claim, doctor, observation, heartbeat, and
release journey in the [remote authority quickstart](remote-claim-authority.md#two-machine-quickstart).

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
credentials, or backup credentials into an image. Import one-time artifacts
into the intended secret manager and remove them according to that system's
policy. A TLS-terminating proxy is optional; if used, follow the server's
trusted-edge requirements rather than passing identity headers as authorization.

## Backup and upgrade

A restart or stop-before-start image upgrade reuses the mounted home unchanged,
which preserves the authority identity, restore identity, claims, replay, and
authentication state. Back up SQLite through a supported online SQLite backup or
replication integration; do not copy a live database file independently of its
WAL state. Optional asynchronous object replication is disaster recovery, not
high availability.

For backup integrations:

- keep replication credentials in read-only secrets;
- keep tool metadata, replica generations, and restore-selection state in a
  separate durable volume;
- never bake that state into the image or mix a writable replica with the
  authority home; and
- restore through `worklease server restore`, which creates a new incarnation
  in recovery mode.

See the [remote authority recovery model](remote-claim-authority.md) for cutoff,
evidence, and reopening requirements.
