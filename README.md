# Worklease

Provider-neutral, same-host coordination for humans and coding agents.
Worklease prevents cooperating local loops from duplicating work. Your backlog
or provider remains authoritative.

![Two workers coordinating ownership of the same task with Worklease](docs/demo.gif)

## Install

Install the latest release with mise:

```toml
[tools]
"github:brettinternet/worklease" = "latest"
```

```sh
mise install
worklease version
```

Or build from source with Go 1.27.1:

```sh
mise run build
./bin/worklease version
```

Release archives are named `worklease-vVERSION-{linux,macos}-{x64,arm64}.tar.gz`
and contain `bin/worklease` and `share/man/man1/worklease.1`. Checksums are in
`checksums.txt`.

## Human CLI quick start

Commands use an owner-private local SQLite authority. A stable session selector
keeps concurrent loops in the same checkout from overwriting each other's
handles. A contextual handle carries the claim ID, revision, authority ID, and
private credential; output never carries the token.

<!-- worklease-example:run human-quick-start -->
```sh
worklease acquire --resource human-quick-start --session human
worklease verify --session human
worklease exec --session human -- worklease version
worklease checkpoint --session human --data '{"phase":"verified"}'
worklease release --session human --reason "provider checkpoint verified"
```
<!-- worklease-example:end -->

Use `--path FILE` to derive exact repository/path membership. Native hooks
confirm only a current claim by default; generate them with `--coverage path`
to require every edited path. A direct `verify --resource RESOURCE` checks exact
membership. Only expected-hash `replace-file` reports
`mutationProtection: local-serialized-replace`; `exec` and provider calls remain
`guarantee: local-coordination`.

## JSON and MCP quick start

Put `--json` before the command for one schema-version 2 envelope. Domain errors
retain a stable `reason`, `exitCode`, and machine-readable `details`; contention
is a normal structured outcome, not a parsing failure.

<!-- worklease-example:run json-two-loops -->
```sh
worklease --json acquire --resource loop-a --session loop-a
worklease --json acquire --resource loop-b --session loop-b
worklease --json status --session loop-a
worklease --json status --session loop-b
worklease --json release --session loop-a --reason done
worklease --json release --session loop-b --reason done
```
<!-- worklease-example:end -->

Contention example:

```sh
worklease --json acquire --resource shared --session contender-a
worklease --json acquire --resource shared --session contender-b
# exits 2 with error.reason "already-claimed" and holder metadata, never a token
worklease --json release --session contender-a --reason done
```

Run the stdio server:

<!-- worklease-example:run mcp-discovery -->
```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"server/discover"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","_meta":{"protocolVersion":"2026-07-28"}}' \
  | worklease mcp
```
<!-- worklease-example:end -->

Handles are private server-side references. MCP intentionally exposes a smaller
surface than the CLI.

For the MCP tool boundary, see [MCP tools](docs/mcp.md).

## Recovery and safety

Every mutation has an exact request and operation ID. Omit `--operation-id` for
a fresh action. Supply one only to replay that same request after a lost
response. A changed request conflicts. Started guarded operations have unknown
outcomes until the authoritative effect and process cessation are established;
inspect and reconcile them explicitly before continuing.

Resources contend by exact bytes in one authority namespace. Local repository
and path identities are host-local. Handles and event/watch cursors are bound to
an immutable authority ID, so copied state cannot silently authorize another
authority. Expiry ends ownership but does not prove an external process stopped.

Credentials may come from a private handle, `--token-file`, or `--token-fd`.
There is no argv bearer-token option. Never put credentials in output, logs,
checkpoints, provider comments, or handoffs.

See:

- [CLI reference](docs/cli-reference.md)
- [Claim, operation, and recovery model](docs/claim-model.md)
- [MCP and JSON](docs/mcp.md)
- [Setup and native hooks](docs/setup.md)
- [Deferred remote-authority proposal](docs/distributed-cloudflare-claim-authority.md)

## Development

```sh
mise run ci
```

`ci` formats, vets, tests, race-tests, scans vulnerabilities, builds the binary,
runs clean-checkout end-to-end smoke, and renders the manual. Release preparation
builds four CGO-disabled archives and verifies their checksums. Publishing needs
separate owner authorization.
