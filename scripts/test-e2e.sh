#!/bin/sh
set -eu

# Clean-checkout Linux/macOS driver. The built-binary smoke covers multi-resource
# claims, path replacement, guarded exec, read views, watch, GC, doctor, setup,
# policy, operation inspection, and MCP negotiation. Package tests cover
# pending/predecessor states and the full tool surface in the regular test tier.
CGO_ENABLED=0 go build -trimpath -o bin/worklease ./cmd/worklease
go run ./cmd/worklease-smoke --binary ./bin/worklease --version dev
go run ./cmd/worklease-remote-smoke --binary ./bin/worklease
go run ./cmd/worklease-doc-test
