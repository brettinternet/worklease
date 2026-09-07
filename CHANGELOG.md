# Changelog

## Unreleased

### Added

- Added the stable `invalid-token` reason (exit code 2) when a current claim ID is paired with an incorrect bearer token. This additive reason is a minor-version API change; `stale-claim` remains reserved for claim ID ownership loss.

### Breaking CLI changes

Short options now have one meaning across the complete command tree. Long options are unchanged. Update scripts as follows:

| Command | Removed short option | Use instead |
| --- | --- | --- |
| global | `-a` (`--help-all`) | `--help-all` |
| `key` | `-s` (`--source`) | `--source` |
| `acquire`, `acquire-bundle` | `-o` (`--owner-id`) | `--owner-id` |
| `gc` | `-r` (`--retention-days`) | `--retention-days` |
| `gc` | `-c` (`--cutoff`) | `--cutoff` |
| `gc` | `-a` (`--apply`) | `--apply` |
| `transfer` | `-C` (`--successor-claim-id`) | `--successor-claim-id` |
| `transfer` | `-O` (`--successor-owner-id`) | `--successor-owner-id` |
| `transfer` | `-W` (`--successor-work-key`) | `--successor-work-key` |
| `list` | `-F` (`--full`) | `--full` |
| `replace-file` | `-p` (`--path`) | `--path` |
| `replace-file` | `-e` (`--expected-sha256`) | `--expected-sha256` |
| `replace-file` | `-C` (`--content-file`) | `--content-file` |

`status --verbose` text labels now use upper snake case, matching the other text renderers (for example, `CLAIM_ID` instead of `claimId`).
