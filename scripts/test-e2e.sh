#!/bin/sh
set -eu

# Clean-checkout Linux/macOS driver. The built-binary smoke covers multi-resource
# claims, path replacement, guarded exec, read views, watch, GC, doctor, setup,
# policy, operation inspection, and MCP negotiation. Native acceptance suites
# create otherwise-unreachable pending/predecessor states and exercise all tools.
CGO_ENABLED=0 go build -trimpath -o bin/worklease ./cmd/worklease
go run ./cmd/worklease-smoke --binary ./bin/worklease --version dev
go run ./cmd/worklease-remote-smoke --binary ./bin/worklease
go run ./cmd/worklease-doc-test
go test ./internal/cli ./internal/mcp -run 'Test(CommandTreeRegistrationHelpAndShortOptions|CanonicalCommandHelpPathsFlagsAndExamples|PendingLifecycleRecoversBeforeAndAfterAuthorityDispatch|LedgerCLIJSONAndPendingHandleReconciliationRecovery|SetupMCPPreviewApplyAndNewUserLifecycle|SetupGuardAndInstructionsJSON|InstructionsAndDoctorAreReadOnly|WatchSubprocessEventWakesFilteredWaiter|PublicFullHistoryAndEventsRedactCheckpointAndCredentials|ReferencesCrossServerPendingRecoveryAndRestartHold|EndToEndDiscoveredClientUsesOnlyLeaseReference|OversizedInputAndExactElevenToolSchemas)$'
