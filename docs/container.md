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

Configuration and TLS material are deployment secrets. Keep them outside the
image, owner-private, and mount them read-only. For example,
`/etc/worklease/server.yaml` can contain:

```yaml
home: /var/lib/worklease
listen: 0.0.0.0:7443
tlsCert: /run/worklease-tls/tls.crt
tlsKey: /run/worklease-tls/tls.key
admittedPrefixes:
  - 'coordination:'
maxTTL: 1h
maxHold: 24h
shutdownTimeout: 10s
healthRate: 100
metadataRate: 100
enrollmentRate: 20
```

Initialize once. The bootstrap output directory is a writable secret sink, not
part of the image or hosted home; import the resulting invite into the intended
secret manager and remove it according to that system's policy.

```sh
docker run --rm \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease \
  --mount type=bind,src=/etc/worklease,dst=/run/worklease,readonly \
  --mount type=bind,src=/etc/worklease/tls,dst=/run/worklease-tls,readonly \
  --mount type=bind,src=/srv/worklease/bootstrap,dst=/run/bootstrap \
  ghcr.io/brettinternet/worklease:v1.2.0 \
  server init --server-config /run/worklease/server.yaml \
  --bootstrap-invite-file /run/bootstrap/admin.invite
```

Run the authority with only the hosted home writable:

```sh
docker run --detach --name worklease --restart unless-stopped \
  --publish 7443:7443 \
  --mount type=bind,src=/srv/worklease/home,dst=/var/lib/worklease \
  --mount type=bind,src=/etc/worklease,dst=/run/worklease,readonly \
  --mount type=bind,src=/etc/worklease/tls,dst=/run/worklease-tls,readonly \
  ghcr.io/brettinternet/worklease:v1.2.0 \
  serve --server-config /run/worklease/server.yaml
```

Do not bake configuration, TLS private keys, invite secrets, installation
credentials, or backup credentials into an image. A TLS-terminating proxy is
optional; if used, follow the server's trusted-edge requirements rather than
passing identity headers as authorization.

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
