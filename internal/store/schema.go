package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/brettinternet/worklease/internal/reason"
)

var requiredTables = map[string]bool{
	"meta": true, "claims": true, "claim_resources": true, "epochs": true,
	"epoch_resources": true, "operations": true, "reconciliations": true, "events": true,
	"recovery_state": true, "installations": true, "invites": true,
	"invite_redemptions": true, "recovery_reopenings": true, "operation_renewals": true,
	"admin_operation_replays": true,
}
var legacyV2RequiredTables = withoutRequired(requiredTables, "admin_operation_replays")
var legacyV2RequiredFragments = withoutRequiredSlices(requiredTableFragments, "admin_operation_replays")
var requiredIndexes = map[string]bool{
	"claim_resources_by_claim": true, "epochs_by_acquired_seq": true,
	"epoch_resources_by_resource": true, "operations_by_state": true,
	"one_started_per_claim": true, "events_by_claim": true, "events_by_at": true,
	"installations_by_credential": true, "invites_by_hash": true,
	"invites_by_state_expiry": true, "invites_by_issuer_operation": true,
	"one_active_bootstrap_invite": true, "recovery_reopenings_by_restore": true,
	"operation_renewals_by_retention":     true,
	"admin_operation_replays_by_deadline": true,
}
var legacyV2RequiredIndexes = withoutRequired(requiredIndexes, "admin_operation_replays_by_deadline")

var requiredTableFragments = map[string][]string{
	"claims": {
		"check (guarantee = 'local-coordination')", "check (local_replace_allowed in (0,1))",
		"check (remote in (0,1))", "check (admitted_ttl_us is null or admitted_ttl_us > 0)",
		"check (admitted_hold_until is null or admitted_hold_until >= acquired_at)",
	},
	"claim_resources": {"references claims(claim_id) on delete cascade"},
	"epochs": {
		"check (end_reason in ('released','transferred','expired','restored','revoked'))",
		"check (remote in (0,1))", "check (admitted_ttl_us is null or admitted_ttl_us > 0)",
		"check (admitted_hold_until is null or admitted_hold_until >= acquired_at)",
	},
	"epoch_resources": {"references epochs(claim_id) on delete cascade", "primary key (claim_id, position)"},
	"operations": {
		"check (state in ('started','completed','reconciled'))", "primary key (claim_id, operation_id)",
		"check (remote in (0,1))",
	},
	"reconciliations": {"check (outcome in ('observed-success','observed-failure'))", "primary key (claim_id, operation_id)", "check (remote in (0,1))"},
	"events":          {"primary key autoincrement", "check (remote in (0,1))"},
	"recovery_state": {
		"check (singleton = 1)", "check (recovery_mode in (0,1))", "check (cutoff_known in (0,1))",
		"check (loss_start_known in (0,1))", "check (loss_end_known in (0,1))", "check (bootstrap_ready in (0,1))",
	},
	"installations": {
		"check (length(installation_id) = 32 and installation_id not glob '*[^0-9a-f]*')",
		"check (role in ('read','write','admin'))",
		"check (length(credential_hash) = 64 and credential_hash not glob '*[^0-9a-f]*')",
		"check ((revoked_at is null and revoked_by_installation_id is null and revoke_reason is null) or revoked_at is not null)",
	},
	"invites": {
		"check (role in ('read','write','admin'))", "check (state in ('active','used','revoked'))",
		"check (bootstrap in (0,1))", "check (length(invite_id) = 32 and invite_id not glob '*[^0-9a-f]*')",
		"check (length(invite_hash) = 64 and invite_hash not glob '*[^0-9a-f]*')",
		"check (expires_at > issued_at)",
		"check (revoked_by_installation_id is null or revoked_at is not null)",
		"check ((state = 'active' and used_at is null and used_by_installation_id is null and revoked_at is null) or (state = 'used' and used_at is not null and used_by_installation_id is not null and revoked_at is null) or (state = 'revoked' and revoked_at is not null))",
	},
	"invite_redemptions": {
		"invite_id text primary key", "check (length(credential_hash) = 64 and credential_hash not glob '*[^0-9a-f]*')",
		"check (replay_until >= redeemed_at and replay_until <= request_not_after)",
	},
	"recovery_reopenings": {
		"check (cutoff_known in (0,1))", "check (loss_start_known in (0,1))", "check (loss_end_known in (0,1))",
		"check (inventory_complete in (0,1))", "check (pending_sets_complete in (0,1))",
		"check (retained_outcomes_complete in (0,1))", "check (namespace_cessation_established in (0,1))",
	},
	"operation_renewals": {
		"primary key (claim_id, operation_id, renewal_id)", "references operations(claim_id, operation_id) on delete cascade", "check (ttl_us > 0)", "check (remote in (0,1))",
	},
	"admin_operation_replays": {
		"primary key (actor_installation_id, operation_id)",
		"check (length(actor_installation_id) = 32 and actor_installation_id not glob '*[^0-9a-f]*')",
		"check (length(operation_id) = 32 and operation_id not glob '*[^0-9a-f]*')",
		"check (length(request_hash) = 64 and request_hash not glob '*[^0-9a-f]*')",
	},
}

func withoutRequired(source map[string]bool, excluded string) map[string]bool {
	copy := make(map[string]bool, len(source)-1)
	for key, value := range source {
		if key != excluded {
			copy[key] = value
		}
	}
	return copy
}

func withoutRequiredSlices(source map[string][]string, excluded string) map[string][]string {
	copy := make(map[string][]string, len(source)-1)
	for key, value := range source {
		if key != excluded {
			copy[key] = value
		}
	}
	return copy
}

var requiredColumns = map[string][]string{
	"meta":                    {"key", "value"},
	"claims":                  {"claim_id", "token_hash", "revision", "agent_id", "session_id", "work_key", "guarantee", "local_replace_allowed", "acquired_at", "ttl_us", "heartbeat_at", "expires_at", "checkpoint", "admitted_ttl_us", "admitted_hold_until", "installation_id", "restore_id", "remote"},
	"claim_resources":         {"resource", "claim_id", "position"},
	"epochs":                  {"claim_id", "token_hash", "agent_id", "session_id", "work_key", "guarantee", "local_replace_allowed", "acquired_at", "acquired_seq", "ended_at", "ended_seq", "ended_recorded_at", "end_reason", "final_revision", "successor_claim_id", "checkpoint", "admitted_ttl_us", "admitted_hold_until", "installation_id", "restore_id", "remote"},
	"epoch_resources":         {"claim_id", "resource", "position"},
	"operations":              {"claim_id", "operation_id", "kind", "request_hash", "request_not_after", "expected_revision", "state", "receipt", "started_at", "started_seq", "completed_at", "completed_seq", "installation_id", "restore_id", "remote"},
	"reconciliations":         {"claim_id", "operation_id", "outcome", "evidence", "request_hash", "reconcile_operation_id", "resolver_claim_id", "resolver_agent_id", "resolver_session_id", "recorded_at", "recorded_seq", "installation_id", "restore_id", "remote"},
	"events":                  {"seq", "at", "kind", "claim_id", "resources", "operation_id", "revision", "agent_id", "detail", "installation_id", "restore_id", "remote"},
	"recovery_state":          {"singleton", "recovery_mode", "recovery_revision", "restored_at", "selected_durable_cutoff", "cutoff_known", "loss_interval_start", "loss_interval_end", "loss_start_known", "loss_end_known", "coverage_gaps", "bootstrap_invite_id", "bootstrap_ready"},
	"installations":           {"installation_id", "credential_hash", "role", "label", "enrolled_at", "restore_id", "enrolled_by_invite_id", "issuer_installation_id", "request_id", "request_hash", "revoked_at", "revoked_by_installation_id", "revoke_reason"},
	"invites":                 {"invite_id", "invite_hash", "role", "label", "issued_at", "expires_at", "restore_id", "issued_by_installation_id", "issue_operation_id", "issue_request_hash", "request_not_after", "bootstrap", "state", "used_at", "used_by_installation_id", "revoked_at", "revoked_by_installation_id"},
	"invite_redemptions":      {"invite_id", "request_id", "request_hash", "request_not_after", "expected_restore_id", "installation_id", "credential_hash", "result", "redeemed_at", "replay_until"},
	"recovery_reopenings":     {"reopening_id", "operation_id", "request_hash", "request_not_after", "restore_id", "recovery_revision", "reopened_at", "reopened_by_installation_id", "selected_durable_cutoff", "cutoff_known", "loss_interval_start", "loss_interval_end", "loss_start_known", "loss_end_known", "inventory_complete", "pending_sets_complete", "retained_outcomes_complete", "namespace_cessation_established", "coverage_gaps", "attestation", "evidence_references"},
	"operation_renewals":      {"claim_id", "operation_id", "renewal_id", "request_hash", "request_not_after", "expected_revision", "ttl_us", "receipt", "renewed_at", "renewed_seq", "installation_id", "restore_id", "remote"},
	"admin_operation_replays": {"actor_installation_id", "operation_id", "request_hash", "request_not_after", "result"},
}

var v1RequiredTables = map[string]bool{
	"meta": true, "claims": true, "claim_resources": true, "epochs": true,
	"epoch_resources": true, "operations": true, "reconciliations": true, "events": true,
}
var v1RequiredIndexes = map[string]bool{
	"claim_resources_by_claim": true, "epochs_by_acquired_seq": true,
	"epoch_resources_by_resource": true, "operations_by_state": true,
	"one_started_per_claim": true, "events_by_claim": true, "events_by_at": true,
}
var v1RequiredColumns = map[string][]string{
	"meta":            {"key", "value"},
	"claims":          {"claim_id", "token_hash", "revision", "agent_id", "session_id", "work_key", "guarantee", "local_replace_allowed", "acquired_at", "ttl_us", "heartbeat_at", "expires_at", "checkpoint"},
	"claim_resources": {"resource", "claim_id", "position"},
	"epochs":          {"claim_id", "token_hash", "agent_id", "session_id", "work_key", "guarantee", "local_replace_allowed", "acquired_at", "acquired_seq", "ended_at", "ended_seq", "ended_recorded_at", "end_reason", "final_revision", "successor_claim_id", "checkpoint"},
	"epoch_resources": {"claim_id", "resource", "position"},
	"operations":      {"claim_id", "operation_id", "kind", "request_hash", "request_not_after", "expected_revision", "state", "receipt", "started_at", "started_seq", "completed_at", "completed_seq"},
	"reconciliations": {"claim_id", "operation_id", "outcome", "evidence", "request_hash", "reconcile_operation_id", "resolver_claim_id", "resolver_agent_id", "resolver_session_id", "recorded_at", "recorded_seq"},
	"events":          {"seq", "at", "kind", "claim_id", "resources", "operation_id", "revision", "agent_id", "detail"},
}
var v1RequiredTableFragments = map[string][]string{
	"claims":          {"check (guarantee = 'local-coordination')", "check (local_replace_allowed in (0,1))"},
	"claim_resources": {"references claims(claim_id) on delete cascade"},
	"epochs":          {"check (end_reason in ('released','transferred','expired'))"},
	"epoch_resources": {"references epochs(claim_id) on delete cascade", "primary key (claim_id, position)"},
	"operations":      {"check (state in ('started','completed','reconciled'))", "primary key (claim_id, operation_id)"},
	"reconciliations": {"check (outcome in ('observed-success','observed-failure'))", "primary key (claim_id, operation_id)"},
	"events":          {"primary key autoincrement"},
}

// beforeMigrationStatementHook is test-only failure injection. Returning an
// error proves that every preceding schema mutation rolls back with the same
// BEGIN IMMEDIATE transaction.
var beforeMigrationStatementHook func(int, string) error
var beforeV2ExtensionStatementHook func(int, string) error

func createSchema(tx *sql.Tx, authority, restore string, now int64) error {
	statements := v2SchemaStatements()
	for i, statement := range statements {
		var err error
		switch {
		case strings.HasPrefix(statement, "INSERT INTO meta"):
			_, err = tx.Exec(statement, now, authority, restore, now)
		default:
			_, err = tx.Exec(statement)
		}
		if err != nil {
			return fmt.Errorf("create schema statement %d: %w", i, err)
		}
	}
	return verifySchemaObjects(tx, requiredTables, requiredIndexes, requiredTableFragments, requiredColumns, true)
}

func v2SchemaStatements() []string {
	return []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE claims (
			claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, revision INTEGER NOT NULL,
			agent_id TEXT NOT NULL, session_id TEXT NOT NULL, work_key TEXT NOT NULL,
			guarantee TEXT NOT NULL CHECK (guarantee = 'local-coordination'),
			local_replace_allowed INTEGER NOT NULL CHECK (local_replace_allowed IN (0,1)),
			acquired_at INTEGER NOT NULL, ttl_us INTEGER NOT NULL, heartbeat_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL, checkpoint TEXT,
			admitted_ttl_us INTEGER CHECK (admitted_ttl_us IS NULL OR admitted_ttl_us > 0),
			admitted_hold_until INTEGER CHECK (admitted_hold_until IS NULL OR admitted_hold_until >= acquired_at),
			installation_id TEXT, restore_id TEXT, remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1)))`,
		`CREATE TABLE claim_resources (
			resource TEXT PRIMARY KEY, claim_id TEXT NOT NULL REFERENCES claims(claim_id) ON DELETE CASCADE,
			position INTEGER NOT NULL)`,
		`CREATE INDEX claim_resources_by_claim ON claim_resources(claim_id)`,
		`CREATE TABLE epochs (
			claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, agent_id TEXT NOT NULL,
			session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL,
			local_replace_allowed INTEGER NOT NULL, acquired_at INTEGER NOT NULL, acquired_seq INTEGER NOT NULL,
			ended_at INTEGER, ended_seq INTEGER, ended_recorded_at INTEGER,
			end_reason TEXT CHECK (end_reason IN ('released','transferred','expired','restored','revoked')),
			final_revision INTEGER, successor_claim_id TEXT, checkpoint TEXT,
			admitted_ttl_us INTEGER CHECK (admitted_ttl_us IS NULL OR admitted_ttl_us > 0),
			admitted_hold_until INTEGER CHECK (admitted_hold_until IS NULL OR admitted_hold_until >= acquired_at),
			installation_id TEXT, restore_id TEXT, remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1)))`,
		`CREATE INDEX epochs_by_acquired_seq ON epochs(acquired_seq)`,
		`CREATE TABLE epoch_resources (
			claim_id TEXT NOT NULL REFERENCES epochs(claim_id) ON DELETE CASCADE,
			resource TEXT NOT NULL, position INTEGER NOT NULL, PRIMARY KEY (claim_id, position))`,
		`CREATE INDEX epoch_resources_by_resource ON epoch_resources(resource, claim_id)`,
		`CREATE TABLE operations (
			claim_id TEXT NOT NULL, operation_id TEXT NOT NULL, kind TEXT NOT NULL,
			request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL, expected_revision INTEGER NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('started','completed','reconciled')), receipt TEXT,
			started_at INTEGER NOT NULL, started_seq INTEGER NOT NULL, completed_at INTEGER, completed_seq INTEGER,
			installation_id TEXT, restore_id TEXT, remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1)),
			PRIMARY KEY (claim_id, operation_id))`,
		`CREATE INDEX operations_by_state ON operations(state)`,
		`CREATE UNIQUE INDEX one_started_per_claim ON operations(claim_id) WHERE state = 'started'`,
		`CREATE TABLE reconciliations (
			claim_id TEXT NOT NULL, operation_id TEXT NOT NULL,
			outcome TEXT NOT NULL CHECK (outcome IN ('observed-success','observed-failure')),
			evidence TEXT NOT NULL, request_hash TEXT NOT NULL, reconcile_operation_id TEXT NOT NULL,
			resolver_claim_id TEXT NOT NULL, resolver_agent_id TEXT NOT NULL, resolver_session_id TEXT NOT NULL,
			recorded_at INTEGER NOT NULL, recorded_seq INTEGER NOT NULL,
			installation_id TEXT, restore_id TEXT, remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1)),
			PRIMARY KEY (claim_id, operation_id))`,
		`CREATE TABLE events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, kind TEXT NOT NULL, claim_id TEXT,
			resources TEXT NOT NULL, operation_id TEXT, revision INTEGER, agent_id TEXT,
			detail TEXT NOT NULL DEFAULT '{}', installation_id TEXT, restore_id TEXT,
			remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1)))`,
		`CREATE INDEX events_by_claim ON events(claim_id, seq)`,
		`CREATE INDEX events_by_at ON events(at)`,
		`CREATE TABLE recovery_state (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			recovery_mode INTEGER NOT NULL DEFAULT 0 CHECK (recovery_mode IN (0,1)),
			recovery_revision INTEGER NOT NULL DEFAULT 0 CHECK (recovery_revision >= 0),
			restored_at INTEGER, selected_durable_cutoff INTEGER,
			cutoff_known INTEGER NOT NULL DEFAULT 1 CHECK (cutoff_known IN (0,1)),
			loss_interval_start INTEGER, loss_interval_end INTEGER,
			loss_start_known INTEGER NOT NULL DEFAULT 1 CHECK (loss_start_known IN (0,1)),
			loss_end_known INTEGER NOT NULL DEFAULT 1 CHECK (loss_end_known IN (0,1)),
			coverage_gaps TEXT NOT NULL DEFAULT '[]', bootstrap_invite_id TEXT,
			bootstrap_ready INTEGER NOT NULL DEFAULT 0 CHECK (bootstrap_ready IN (0,1)))`,
		`CREATE TABLE installations (
			installation_id TEXT PRIMARY KEY CHECK (length(installation_id) = 32 AND installation_id NOT GLOB '*[^0-9a-f]*'),
			credential_hash TEXT NOT NULL UNIQUE CHECK (length(credential_hash) = 64 AND credential_hash NOT GLOB '*[^0-9a-f]*'),
			role TEXT NOT NULL CHECK (role IN ('read','write','admin')), label TEXT NOT NULL,
			enrolled_at INTEGER NOT NULL, restore_id TEXT NOT NULL, enrolled_by_invite_id TEXT NOT NULL,
			issuer_installation_id TEXT, request_id TEXT NOT NULL, request_hash TEXT NOT NULL,
			revoked_at INTEGER, revoked_by_installation_id TEXT, revoke_reason TEXT,
			CHECK ((revoked_at IS NULL AND revoked_by_installation_id IS NULL AND revoke_reason IS NULL) OR revoked_at IS NOT NULL))`,
		`CREATE UNIQUE INDEX installations_by_credential ON installations(credential_hash)`,
		`CREATE TABLE invites (
			invite_id TEXT PRIMARY KEY CHECK (length(invite_id) = 32 AND invite_id NOT GLOB '*[^0-9a-f]*'),
			invite_hash TEXT NOT NULL UNIQUE CHECK (length(invite_hash) = 64 AND invite_hash NOT GLOB '*[^0-9a-f]*'),
			role TEXT NOT NULL CHECK (role IN ('read','write','admin')), label TEXT NOT NULL,
			issued_at INTEGER NOT NULL, expires_at INTEGER NOT NULL CHECK (expires_at > issued_at),
			restore_id TEXT NOT NULL, issued_by_installation_id TEXT,
			issue_operation_id TEXT NOT NULL, issue_request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL,
			bootstrap INTEGER NOT NULL DEFAULT 0 CHECK (bootstrap IN (0,1)),
			state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','used','revoked')),
			used_at INTEGER, used_by_installation_id TEXT, revoked_at INTEGER, revoked_by_installation_id TEXT,
			CHECK (revoked_by_installation_id IS NULL OR revoked_at IS NOT NULL),
			CHECK ((state = 'active' AND used_at IS NULL AND used_by_installation_id IS NULL AND revoked_at IS NULL)
				OR (state = 'used' AND used_at IS NOT NULL AND used_by_installation_id IS NOT NULL AND revoked_at IS NULL)
				OR (state = 'revoked' AND revoked_at IS NOT NULL)))`,
		`CREATE UNIQUE INDEX invites_by_hash ON invites(invite_hash)`,
		`CREATE INDEX invites_by_state_expiry ON invites(state, expires_at)`,
		`CREATE UNIQUE INDEX invites_by_issuer_operation ON invites(issued_by_installation_id, issue_operation_id)`,
		`CREATE UNIQUE INDEX one_active_bootstrap_invite ON invites(bootstrap) WHERE bootstrap = 1 AND state = 'active'`,
		`CREATE TABLE invite_redemptions (
			invite_id TEXT PRIMARY KEY REFERENCES invites(invite_id) ON DELETE RESTRICT,
			request_id TEXT NOT NULL, request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL,
			expected_restore_id TEXT NOT NULL, installation_id TEXT NOT NULL,
			credential_hash TEXT NOT NULL CHECK (length(credential_hash) = 64 AND credential_hash NOT GLOB '*[^0-9a-f]*'), result TEXT NOT NULL,
			redeemed_at INTEGER NOT NULL, replay_until INTEGER NOT NULL,
			CHECK (replay_until >= redeemed_at AND replay_until <= request_not_after))`,
		`CREATE TABLE recovery_reopenings (
			reopening_id INTEGER PRIMARY KEY AUTOINCREMENT, operation_id TEXT NOT NULL UNIQUE,
			request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL, restore_id TEXT NOT NULL,
			recovery_revision INTEGER NOT NULL, reopened_at INTEGER NOT NULL,
			reopened_by_installation_id TEXT NOT NULL, selected_durable_cutoff INTEGER,
			cutoff_known INTEGER NOT NULL CHECK (cutoff_known IN (0,1)),
			loss_interval_start INTEGER, loss_interval_end INTEGER,
			loss_start_known INTEGER NOT NULL CHECK (loss_start_known IN (0,1)),
			loss_end_known INTEGER NOT NULL CHECK (loss_end_known IN (0,1)),
			inventory_complete INTEGER NOT NULL CHECK (inventory_complete IN (0,1)),
			pending_sets_complete INTEGER NOT NULL CHECK (pending_sets_complete IN (0,1)),
			retained_outcomes_complete INTEGER NOT NULL CHECK (retained_outcomes_complete IN (0,1)),
			namespace_cessation_established INTEGER NOT NULL CHECK (namespace_cessation_established IN (0,1)),
			coverage_gaps TEXT NOT NULL DEFAULT '[]', attestation TEXT NOT NULL, evidence_references TEXT NOT NULL DEFAULT '[]')`,
		`CREATE INDEX recovery_reopenings_by_restore ON recovery_reopenings(restore_id, reopening_id)`,
		`CREATE TABLE operation_renewals (
			claim_id TEXT NOT NULL, operation_id TEXT NOT NULL, renewal_id TEXT NOT NULL,
			request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL, expected_revision INTEGER NOT NULL,
			ttl_us INTEGER NOT NULL CHECK (ttl_us > 0), receipt TEXT NOT NULL,
			renewed_at INTEGER NOT NULL, renewed_seq INTEGER NOT NULL,
			installation_id TEXT NOT NULL, restore_id TEXT NOT NULL,
			remote INTEGER NOT NULL DEFAULT 1 CHECK (remote IN (0,1)),
			PRIMARY KEY (claim_id, operation_id, renewal_id),
			FOREIGN KEY (claim_id, operation_id) REFERENCES operations(claim_id, operation_id) ON DELETE CASCADE)`,
		`CREATE INDEX operation_renewals_by_retention ON operation_renewals(request_not_after, claim_id, operation_id)`,
		`CREATE TABLE admin_operation_replays (
			actor_installation_id TEXT NOT NULL CHECK (length(actor_installation_id) = 32 AND actor_installation_id NOT GLOB '*[^0-9a-f]*'),
			operation_id TEXT NOT NULL CHECK (length(operation_id) = 32 AND operation_id NOT GLOB '*[^0-9a-f]*'),
			request_hash TEXT NOT NULL CHECK (length(request_hash) = 64 AND request_hash NOT GLOB '*[^0-9a-f]*'),
			request_not_after INTEGER NOT NULL, result TEXT NOT NULL,
			PRIMARY KEY (actor_installation_id, operation_id))`,
		`CREATE INDEX admin_operation_replays_by_deadline ON admin_operation_replays(request_not_after)`,
		`INSERT INTO recovery_state(singleton) VALUES(1)`,
		`INSERT INTO meta(key,value) VALUES
			('created_at', ?), ('authority_id', ?), ('restore_id', ?), ('last_observed_at', ?),
			('last_event_seq', '0'), ('pruned_through_seq', '0'), ('schema_v2_admin_operation_replays', '1')`,
		`PRAGMA user_version = 2`,
	}
}

func v2StandaloneSchemaStatements() []string {
	var standalone []string
	for _, statement := range v2SchemaStatements() {
		for _, prefix := range []string{
			"CREATE TABLE recovery_state", "CREATE TABLE installations", "CREATE UNIQUE INDEX installations_by_credential",
			"CREATE TABLE invites", "CREATE UNIQUE INDEX invites_by_hash", "CREATE INDEX invites_by_state_expiry",
			"CREATE UNIQUE INDEX invites_by_issuer_operation", "CREATE UNIQUE INDEX one_active_bootstrap_invite",
			"CREATE TABLE invite_redemptions", "CREATE TABLE recovery_reopenings", "CREATE INDEX recovery_reopenings_by_restore",
			"CREATE TABLE operation_renewals", "CREATE INDEX operation_renewals_by_retention",
			"CREATE TABLE admin_operation_replays", "CREATE INDEX admin_operation_replays_by_deadline",
		} {
			if strings.HasPrefix(statement, prefix) {
				standalone = append(standalone, statement)
				break
			}
		}
	}
	return standalone
}

func migrateSchemaV1(tx *sql.Tx, restore string) error {
	if err := verifySchemaObjects(tx, v1RequiredTables, v1RequiredIndexes, v1RequiredTableFragments, v1RequiredColumns, false); err != nil {
		return err
	}
	statements := []string{
		`ALTER TABLE claims ADD COLUMN admitted_ttl_us INTEGER CHECK (admitted_ttl_us IS NULL OR admitted_ttl_us > 0)`,
		`ALTER TABLE claims ADD COLUMN admitted_hold_until INTEGER CHECK (admitted_hold_until IS NULL OR admitted_hold_until >= acquired_at)`,
		`ALTER TABLE claims ADD COLUMN installation_id TEXT`,
		`ALTER TABLE claims ADD COLUMN restore_id TEXT`,
		`ALTER TABLE claims ADD COLUMN remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1))`,
		`DROP INDEX epochs_by_acquired_seq`,
		`DROP INDEX epoch_resources_by_resource`,
		`ALTER TABLE epoch_resources RENAME TO epoch_resources_v1`,
		`ALTER TABLE epochs RENAME TO epochs_v1`,
		`CREATE TABLE epochs (
			claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, agent_id TEXT NOT NULL,
			session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL,
			local_replace_allowed INTEGER NOT NULL, acquired_at INTEGER NOT NULL, acquired_seq INTEGER NOT NULL,
			ended_at INTEGER, ended_seq INTEGER, ended_recorded_at INTEGER,
			end_reason TEXT CHECK (end_reason IN ('released','transferred','expired','restored','revoked')),
			final_revision INTEGER, successor_claim_id TEXT, checkpoint TEXT,
			admitted_ttl_us INTEGER CHECK (admitted_ttl_us IS NULL OR admitted_ttl_us > 0),
			admitted_hold_until INTEGER CHECK (admitted_hold_until IS NULL OR admitted_hold_until >= acquired_at),
			installation_id TEXT, restore_id TEXT, remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1)))`,
		`CREATE INDEX epochs_by_acquired_seq ON epochs(acquired_seq)`,
		`CREATE TABLE epoch_resources (
			claim_id TEXT NOT NULL REFERENCES epochs(claim_id) ON DELETE CASCADE,
			resource TEXT NOT NULL, position INTEGER NOT NULL, PRIMARY KEY (claim_id, position))`,
		`CREATE INDEX epoch_resources_by_resource ON epoch_resources(resource, claim_id)`,
		`INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,ended_seq,ended_recorded_at,end_reason,final_revision,successor_claim_id,checkpoint)
			SELECT claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,ended_seq,ended_recorded_at,end_reason,final_revision,successor_claim_id,checkpoint FROM epochs_v1`,
		`INSERT INTO epoch_resources(claim_id,resource,position) SELECT claim_id,resource,position FROM epoch_resources_v1`,
		`DROP TABLE epoch_resources_v1`,
		`DROP TABLE epochs_v1`,
		`ALTER TABLE operations ADD COLUMN installation_id TEXT`,
		`ALTER TABLE operations ADD COLUMN restore_id TEXT`,
		`ALTER TABLE operations ADD COLUMN remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1))`,
		`ALTER TABLE reconciliations ADD COLUMN installation_id TEXT`,
		`ALTER TABLE reconciliations ADD COLUMN restore_id TEXT`,
		`ALTER TABLE reconciliations ADD COLUMN remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1))`,
		`ALTER TABLE events ADD COLUMN installation_id TEXT`,
		`ALTER TABLE events ADD COLUMN restore_id TEXT`,
		`ALTER TABLE events ADD COLUMN remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0,1))`,
	}
	// Reuse only standalone v2 objects; do not depend on positional slices of
	// the complete fresh-schema statement list.
	statements = append(statements, v2StandaloneSchemaStatements()...)
	statements = append(statements,
		`INSERT INTO recovery_state(singleton) VALUES(1)`,
		`INSERT INTO meta(key,value) VALUES('restore_id', ?), ('schema_v2_admin_operation_replays', '1')`,
		`PRAGMA user_version = 2`,
	)
	for i, statement := range statements {
		if beforeMigrationStatementHook != nil {
			if err := beforeMigrationStatementHook(i, statement); err != nil {
				return err
			}
		}
		var err error
		if strings.Contains(statement, "VALUES('restore_id', ?)") {
			_, err = tx.Exec(statement, restore)
		} else {
			_, err = tx.Exec(statement)
		}
		if err != nil {
			return fmt.Errorf("migrate schema statement %d: %w", i, err)
		}
	}
	return verifySchemaObjects(tx, requiredTables, requiredIndexes, requiredTableFragments, requiredColumns, true)
}

type schemaQueryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

const adminReplaySchemaMarker = "schema_v2_admin_operation_replays"

func ensureV2AdminReplaySchema(ctx context.Context, driver *Driver) error {
	return driver.Write(ctx, func(tx *sql.Tx) error {
		var marker string
		err := tx.QueryRow(`SELECT value FROM meta WHERE key=?`, adminReplaySchemaMarker).Scan(&marker)
		if err == nil {
			if marker != "1" {
				return reason.New(reason.ReasonSchemaCorrupt, "admin replay schema marker is invalid")
			}
			return verifySchemaObjects(tx, requiredTables, requiredIndexes, requiredTableFragments, requiredColumns, true)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return schemaCorrupt(err)
		}
		if err := verifySchemaObjects(tx, legacyV2RequiredTables, legacyV2RequiredIndexes, legacyV2RequiredFragments, requiredColumnsWithout(requiredColumns, "admin_operation_replays"), true); err != nil {
			return err
		}
		for index, statement := range []string{
			`CREATE TABLE admin_operation_replays (actor_installation_id TEXT NOT NULL CHECK (length(actor_installation_id) = 32 AND actor_installation_id NOT GLOB '*[^0-9a-f]*'), operation_id TEXT NOT NULL CHECK (length(operation_id) = 32 AND operation_id NOT GLOB '*[^0-9a-f]*'), request_hash TEXT NOT NULL CHECK (length(request_hash) = 64 AND request_hash NOT GLOB '*[^0-9a-f]*'), request_not_after INTEGER NOT NULL, result TEXT NOT NULL, PRIMARY KEY (actor_installation_id, operation_id))`,
			`CREATE INDEX admin_operation_replays_by_deadline ON admin_operation_replays(request_not_after)`,
			`INSERT INTO meta(key,value) VALUES(?, '1')`,
		} {
			if beforeV2ExtensionStatementHook != nil {
				if err := beforeV2ExtensionStatementHook(index, statement); err != nil {
					return err
				}
			}
			var execErr error
			if strings.HasPrefix(statement, "INSERT INTO meta") {
				_, execErr = tx.Exec(statement, adminReplaySchemaMarker)
			} else {
				_, execErr = tx.Exec(statement)
			}
			if execErr != nil {
				return schemaCorrupt(execErr)
			}
		}
		return verifySchemaObjects(tx, requiredTables, requiredIndexes, requiredTableFragments, requiredColumns, true)
	})
}

func requiredColumnsWithout(source map[string][]string, excluded string) map[string][]string {
	copy := make(map[string][]string, len(source)-1)
	for key, value := range source {
		if key != excluded {
			copy[key] = value
		}
	}
	return copy
}

func verifySchema(db *sql.DB) error {
	return verifySchemaObjects(db, requiredTables, requiredIndexes, requiredTableFragments, requiredColumns, true)
}

// verifyReadOnlyV2Schema accepts an unextended v2 database without mutating it.
// Once the extension marker exists, however, the complete extension is required.
func verifyReadOnlyV2Schema(db *sql.DB) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM meta WHERE key=?`, adminReplaySchemaMarker).Scan(&marker)
	if err == nil {
		if marker != "1" {
			return reason.New(reason.ReasonSchemaCorrupt, "admin replay schema marker is invalid")
		}
		return verifySchema(db)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return schemaCorrupt(err)
	}
	var tableCount int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='admin_operation_replays'`).Scan(&tableCount); err != nil {
		return schemaCorrupt(err)
	}
	if tableCount != 0 {
		return reason.New(reason.ReasonSchemaCorrupt, "admin replay schema marker is missing")
	}
	return verifySchemaObjects(db, legacyV2RequiredTables, legacyV2RequiredIndexes, legacyV2RequiredFragments, requiredColumnsWithout(requiredColumns, "admin_operation_replays"), true)
}

func verifySchemaObjects(db schemaQueryer, tables, indexes map[string]bool, fragments map[string][]string, columns map[string][]string, verifyMeta bool) error {
	rows, err := db.Query(`SELECT name, type, coalesce(sql,'') FROM sqlite_master WHERE type IN ('table','index')`)
	if err != nil {
		return schemaCorrupt(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	definitions := map[string]string{}
	for rows.Next() {
		var name, typ, definition string
		if err := rows.Scan(&name, &typ, &definition); err != nil {
			return schemaCorrupt(err)
		}
		seen[name] = typ
		definitions[name] = normalizeSQL(definition)
	}
	if err := rows.Err(); err != nil {
		return schemaCorrupt(err)
	}
	for name := range tables {
		if seen[name] != "table" {
			return reason.New(reason.ReasonSchemaCorrupt, "required table is missing").With("table", name)
		}
	}
	for name := range indexes {
		if seen[name] != "index" {
			return reason.New(reason.ReasonSchemaCorrupt, "required index is missing").With("index", name)
		}
	}
	for table, required := range fragments {
		for _, fragment := range required {
			if !strings.Contains(definitions[table], normalizeSQL(fragment)) {
				return reason.New(reason.ReasonSchemaCorrupt, "required table constraint is missing").With("table", table)
			}
		}
	}
	if err := verifyStartedOperationIndex(db, definitions["one_started_per_claim"]); err != nil {
		return err
	}
	if tables["admin_operation_replays"] {
		if err := verifyIndexColumns(db, "admin_operation_replays", "admin_operation_replays_by_deadline", []string{"request_not_after"}); err != nil {
			return err
		}
	}
	if tables["invites"] {
		checks := []struct {
			table, name string
			columns     []string
			partial     bool
		}{
			{"installations", "installations_by_credential", []string{"credential_hash"}, false},
			{"invites", "invites_by_hash", []string{"invite_hash"}, false},
			{"invites", "invites_by_issuer_operation", []string{"issued_by_installation_id", "issue_operation_id"}, false},
			{"invites", "one_active_bootstrap_invite", []string{"bootstrap"}, true},
		}
		for _, check := range checks {
			if err := verifyUniqueIndex(db, check.table, check.name, check.columns, check.partial); err != nil {
				return err
			}
		}
		if !strings.Contains(definitions["one_active_bootstrap_invite"], "where bootstrap = 1 and state = 'active'") {
			return reason.New(reason.ReasonSchemaCorrupt, "active bootstrap invite constraint is invalid")
		}
	}
	for table, required := range columns {
		query := `PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`
		columnRows, err := db.Query(query)
		if err != nil {
			return schemaCorrupt(err)
		}
		got := map[string]bool{}
		for columnRows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt any
			if err := columnRows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				columnRows.Close()
				return schemaCorrupt(err)
			}
			got[name] = true
		}
		if err := columnRows.Close(); err != nil {
			return schemaCorrupt(err)
		}
		for _, column := range required {
			if !got[column] {
				return reason.New(reason.ReasonSchemaCorrupt, "required column is missing").With("table", table).With("column", column)
			}
		}
	}
	if verifyMeta {
		for key := range map[string]bool{"created_at": true, "authority_id": true, "restore_id": true, "last_observed_at": true, "last_event_seq": true, "pruned_through_seq": true} {
			var value string
			if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil || value == "" {
				return reason.New(reason.ReasonSchemaCorrupt, "required metadata is missing").With("key", key)
			}
			if key == "authority_id" || key == "restore_id" {
				if !validAuthorityID(value) {
					return reason.New(reason.ReasonSchemaCorrupt, "authority identity is invalid").With("key", key)
				}
			} else if _, err := parseDecimal(value); err != nil {
				return reason.New(reason.ReasonSchemaCorrupt, "metadata watermark is not a decimal").With("key", key)
			}
		}
	}
	return nil
}

func verifyStartedOperationIndex(db schemaQueryer, definition string) error {
	rows, err := db.Query(`PRAGMA index_list("operations")`)
	if err != nil {
		return schemaCorrupt(err)
	}
	defer rows.Close()
	valid := false
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			return schemaCorrupt(err)
		}
		if name == "one_started_per_claim" {
			valid = unique == 1 && partial == 1 && strings.Contains(definition, "where state = 'started'")
		}
	}
	if err := rows.Err(); err != nil {
		return schemaCorrupt(err)
	}
	if !valid {
		return reason.New(reason.ReasonSchemaCorrupt, "started-operation uniqueness constraint is invalid")
	}
	var columns int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_index_info('one_started_per_claim') WHERE name='claim_id'`).Scan(&columns); err != nil || columns != 1 {
		return reason.New(reason.ReasonSchemaCorrupt, "started-operation index columns are invalid")
	}
	return nil
}

func verifyIndexColumns(db schemaQueryer, table, name string, columns []string) error {
	rows, err := db.Query(`PRAGMA index_info("` + strings.ReplaceAll(name, `"`, `""`) + `")`)
	if err != nil {
		return schemaCorrupt(err)
	}
	defer rows.Close()
	got := make([]string, 0, len(columns))
	for rows.Next() {
		var seq, cid int
		var column string
		if err := rows.Scan(&seq, &cid, &column); err != nil {
			return schemaCorrupt(err)
		}
		got = append(got, column)
	}
	if err := rows.Err(); err != nil {
		return schemaCorrupt(err)
	}
	if len(got) != len(columns) {
		return reason.New(reason.ReasonSchemaCorrupt, "required index columns are invalid").With("index", name)
	}
	for i := range columns {
		if got[i] != columns[i] {
			return reason.New(reason.ReasonSchemaCorrupt, "required index columns are invalid").With("index", name)
		}
	}
	return nil
}

func verifyUniqueIndex(db schemaQueryer, table, name string, columns []string, partialExpected bool) error {
	rows, err := db.Query(`PRAGMA index_list("` + table + `")`)
	if err != nil {
		return schemaCorrupt(err)
	}
	valid := false
	for rows.Next() {
		var sequence, unique, partial int
		var found, origin string
		if err := rows.Scan(&sequence, &found, &unique, &origin, &partial); err != nil {
			rows.Close()
			return schemaCorrupt(err)
		}
		if found == name {
			valid = unique == 1 && (partial == 1) == partialExpected
		}
	}
	if err := rows.Close(); err != nil {
		return schemaCorrupt(err)
	}
	if !valid {
		return reason.New(reason.ReasonSchemaCorrupt, "required uniqueness constraint is invalid").With("index", name)
	}
	columnRows, err := db.Query(`PRAGMA index_info("` + name + `")`)
	if err != nil {
		return schemaCorrupt(err)
	}
	var got []string
	for columnRows.Next() {
		var sequence, cid int
		var column string
		if err := columnRows.Scan(&sequence, &cid, &column); err != nil {
			columnRows.Close()
			return schemaCorrupt(err)
		}
		got = append(got, column)
	}
	if err := columnRows.Close(); err != nil {
		return schemaCorrupt(err)
	}
	if len(got) != len(columns) {
		return reason.New(reason.ReasonSchemaCorrupt, "required uniqueness columns are invalid").With("index", name)
	}
	for i := range columns {
		if got[i] != columns[i] {
			return reason.New(reason.ReasonSchemaCorrupt, "required uniqueness columns are invalid").With("index", name)
		}
	}
	return nil
}

func normalizeSQL(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}
