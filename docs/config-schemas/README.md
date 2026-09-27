# Worklease configuration schemas

The five user-authored Worklease YAML files have [JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema) schemas for editor completion and structural validation:

| File under `$XDG_CONFIG_HOME/worklease/` | Schema |
| --- | --- |
| `config.yaml` | [config.schema.json](config.schema.json) |
| `server.yaml` | [server.schema.json](server.schema.json) |
| `profiles.yaml` | [profiles.schema.json](profiles.schema.json) |
| `bindings.yaml` | [bindings.schema.json](bindings.schema.json) |
| `queue.yaml` | [queue.schema.json](queue.schema.json) |

For a YAML language server, add a first-line comment to an individual private file, for example:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/brettinternet/worklease/main/docs/config-schemas/queue.schema.json
version: 1
```

Or configure your editor to associate the schema URLs with these five filenames in the Worklease configuration directory. No schema URL or editor setting is required at runtime. The schema checks shape and common constraints; Worklease's loaders remain authoritative for file ownership, paths, duration bounds, profile/source references, adapter-specific configuration and other semantic checks. Generated state and credentials are **not** user configuration files; `backlog.config.yml` belongs to Backlog.md, not Worklease.
