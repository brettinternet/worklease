package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/brettinternet/worklease/internal/reason"
)

var requiredTables = map[string]bool{
	"meta": true, "claims": true, "claim_resources": true, "epochs": true,
	"epoch_resources": true, "operations": true, "reconciliations": true, "events": true,
}
var requiredIndexes = map[string]bool{
	"claim_resources_by_claim": true, "epochs_by_acquired_seq": true,
	"epoch_resources_by_resource": true, "operations_by_state": true,
	"one_started_per_claim": true, "events_by_claim": true, "events_by_at": true,
}

var requiredTableFragments = map[string][]string{
	"claims":          {"check (guarantee = 'local-coordination')", "check (local_replace_allowed in (0,1))"},
	"claim_resources": {"references claims(claim_id) on delete cascade"},
	"epochs":          {"check (end_reason in ('released','transferred','expired'))"},
	"epoch_resources": {"references epochs(claim_id) on delete cascade", "primary key (claim_id, position)"},
	"operations":      {"check (state in ('started','completed','reconciled'))", "primary key (claim_id, operation_id)"},
	"reconciliations": {"check (outcome in ('observed-success','observed-failure'))", "primary key (claim_id, operation_id)"},
	"events":          {"primary key autoincrement"},
}

func createSchema(tx *sql.Tx, authority string, now int64) error {
	statements := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE claims (
			claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, revision INTEGER NOT NULL,
			agent_id TEXT NOT NULL, session_id TEXT NOT NULL, work_key TEXT NOT NULL,
			guarantee TEXT NOT NULL CHECK (guarantee = 'local-coordination'),
			local_replace_allowed INTEGER NOT NULL CHECK (local_replace_allowed IN (0,1)),
			acquired_at INTEGER NOT NULL, ttl_us INTEGER NOT NULL, heartbeat_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL, checkpoint TEXT)`,
		`CREATE TABLE claim_resources (
			resource TEXT PRIMARY KEY, claim_id TEXT NOT NULL REFERENCES claims(claim_id) ON DELETE CASCADE,
			position INTEGER NOT NULL)`,
		`CREATE INDEX claim_resources_by_claim ON claim_resources(claim_id)`,
		`CREATE TABLE epochs (
			claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, agent_id TEXT NOT NULL,
			session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL,
			local_replace_allowed INTEGER NOT NULL, acquired_at INTEGER NOT NULL, acquired_seq INTEGER NOT NULL,
			ended_at INTEGER, ended_seq INTEGER, ended_recorded_at INTEGER,
			end_reason TEXT CHECK (end_reason IN ('released','transferred','expired')),
			final_revision INTEGER, successor_claim_id TEXT, checkpoint TEXT)`,
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
			PRIMARY KEY (claim_id, operation_id))`,
		`CREATE INDEX operations_by_state ON operations(state)`,
		`CREATE UNIQUE INDEX one_started_per_claim ON operations(claim_id) WHERE state = 'started'`,
		`CREATE TABLE reconciliations (
			claim_id TEXT NOT NULL, operation_id TEXT NOT NULL,
			outcome TEXT NOT NULL CHECK (outcome IN ('observed-success','observed-failure')),
			evidence TEXT NOT NULL, request_hash TEXT NOT NULL, reconcile_operation_id TEXT NOT NULL,
			resolver_claim_id TEXT NOT NULL, resolver_agent_id TEXT NOT NULL, resolver_session_id TEXT NOT NULL,
			recorded_at INTEGER NOT NULL, recorded_seq INTEGER NOT NULL, PRIMARY KEY (claim_id, operation_id))`,
		`CREATE TABLE events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, kind TEXT NOT NULL, claim_id TEXT,
			resources TEXT NOT NULL, operation_id TEXT, revision INTEGER, agent_id TEXT,
			detail TEXT NOT NULL DEFAULT '{}')`,
		`CREATE INDEX events_by_claim ON events(claim_id, seq)`,
		`CREATE INDEX events_by_at ON events(at)`,
		`INSERT INTO meta(key,value) VALUES
			('created_at', ?), ('authority_id', ?), ('last_observed_at', ?),
			('last_event_seq', '0'), ('pruned_through_seq', '0')`,
		`PRAGMA user_version = 1`,
	}
	for i, statement := range statements {
		if i == len(statements)-2 {
			if _, err := tx.Exec(statement, now, authority, now); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("create schema statement %d: %w", i, err)
		}
	}
	return nil
}

func verifySchema(db *sql.DB) error {
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
	for name := range requiredTables {
		if seen[name] != "table" {
			return reason.New(reason.ReasonSchemaCorrupt, "required table is missing").With("table", name)
		}
	}
	for name := range requiredIndexes {
		if seen[name] != "index" {
			return reason.New(reason.ReasonSchemaCorrupt, "required index is missing").With("index", name)
		}
	}
	for table, fragments := range requiredTableFragments {
		for _, fragment := range fragments {
			if !strings.Contains(definitions[table], normalizeSQL(fragment)) {
				return reason.New(reason.ReasonSchemaCorrupt, "required table constraint is missing").With("table", table)
			}
		}
	}
	if err := verifyStartedOperationIndex(db, definitions["one_started_per_claim"]); err != nil {
		return err
	}
	for table, columns := range map[string][]string{
		"meta": {"key", "value"}, "claims": {"claim_id", "token_hash", "revision", "agent_id", "session_id", "work_key", "guarantee", "local_replace_allowed", "acquired_at", "ttl_us", "heartbeat_at", "expires_at", "checkpoint"},
		"claim_resources": {"resource", "claim_id", "position"}, "epochs": {"claim_id", "token_hash", "agent_id", "session_id", "work_key", "guarantee", "local_replace_allowed", "acquired_at", "acquired_seq", "ended_at", "ended_seq", "ended_recorded_at", "end_reason", "final_revision", "successor_claim_id", "checkpoint"},
		"epoch_resources": {"claim_id", "resource", "position"}, "operations": {"claim_id", "operation_id", "kind", "request_hash", "request_not_after", "expected_revision", "state", "receipt", "started_at", "started_seq", "completed_at", "completed_seq"},
		"reconciliations": {"claim_id", "operation_id", "outcome", "evidence", "request_hash", "reconcile_operation_id", "resolver_claim_id", "resolver_agent_id", "resolver_session_id", "recorded_at", "recorded_seq"},
		"events":          {"seq", "at", "kind", "claim_id", "resources", "operation_id", "revision", "agent_id", "detail"},
	} {
		query := `PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`
		rows, err := db.Query(query)
		if err != nil {
			return schemaCorrupt(err)
		}
		got := map[string]bool{}
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return schemaCorrupt(err)
			}
			got[name] = true
		}
		closeErr := rows.Close()
		if closeErr != nil {
			return schemaCorrupt(closeErr)
		}
		for _, column := range columns {
			if !got[column] {
				return reason.New(reason.ReasonSchemaCorrupt, "required column is missing").With("table", table).With("column", column)
			}
		}
	}
	for key := range map[string]bool{"created_at": true, "authority_id": true, "last_observed_at": true, "last_event_seq": true, "pruned_through_seq": true} {
		var value string
		if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil || value == "" {
			return reason.New(reason.ReasonSchemaCorrupt, "required metadata is missing").With("key", key)
		}
		if key == "authority_id" {
			if !validAuthorityID(value) {
				return reason.New(reason.ReasonSchemaCorrupt, "authority identity is invalid").With("key", key)
			}
		} else if _, err := parseDecimal(value); err != nil {
			return reason.New(reason.ReasonSchemaCorrupt, "metadata watermark is not a decimal").With("key", key)
		}
	}
	return nil
}

func verifyStartedOperationIndex(db *sql.DB, definition string) error {
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

func normalizeSQL(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}
