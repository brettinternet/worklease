"""SQLite persistence setup for worklease."""

from __future__ import annotations

import fcntl
import os
import sqlite3
import stat
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path


def lease_home(home: str | os.PathLike[str] | None = None) -> Path:
    """Return the isolated state directory used by worklease."""

    if home is not None:
        return Path(home).expanduser().resolve()
    override = os.environ.get("WORKLEASE_HOME")
    if override:
        return Path(override).expanduser().resolve()
    state_home = Path(
        os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local" / "state")
    )
    return (state_home / "worklease").expanduser().resolve()


def secure_directory(path: Path) -> Path:
    """Create a private state directory or tighten an existing one."""

    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.chmod(0o700)
    return path


def open_private_file(path: Path) -> int:
    """Open one regular state file without following a planted symlink."""

    flags = os.O_CREAT | os.O_RDWR | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags, 0o600)
    if not stat.S_ISREG(os.fstat(descriptor).st_mode):
        os.close(descriptor)
        raise OSError(f"state path is not a regular file: {path}")
    os.fchmod(descriptor, 0o600)
    return descriptor


@contextmanager
def database_setup_lock(home: Path) -> Iterator[None]:
    secure_directory(home)
    descriptor = open_private_file(home / "database.lock")
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        yield
    finally:
        os.close(descriptor)


def _schema(connection: sqlite3.Connection, home: Path) -> None:
    with database_setup_lock(home):
        connection.execute("PRAGMA journal_mode = WAL")
        connection.execute("PRAGMA synchronous = FULL")
        connection.executescript(
            """
            CREATE TABLE IF NOT EXISTS schema_meta (
                version INTEGER PRIMARY KEY
            );
            INSERT OR IGNORE INTO schema_meta(version) VALUES (1);

            CREATE TABLE IF NOT EXISTS epochs (
                claim_id TEXT PRIMARY KEY,
                resource TEXT NOT NULL,
                agent_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                owner_id TEXT NOT NULL,
                work_key TEXT NOT NULL,
                acquired_at REAL NOT NULL
            );

            CREATE TABLE IF NOT EXISTS resources (
                resource TEXT PRIMARY KEY,
                revision INTEGER NOT NULL
            );

            CREATE TABLE IF NOT EXISTS claims (
                resource TEXT PRIMARY KEY,
                claim_id TEXT NOT NULL,
                token TEXT NOT NULL,
                revision INTEGER NOT NULL,
                agent_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                owner_id TEXT NOT NULL,
                work_key TEXT NOT NULL,
                coordination_only INTEGER NOT NULL DEFAULT 0,
                acquired_at REAL NOT NULL,
                acquire_ttl REAL NOT NULL,
                heartbeat_at REAL NOT NULL,
                expires_at REAL NOT NULL,
                checkpoint TEXT
            );

            CREATE TABLE IF NOT EXISTS operations (
                resource TEXT NOT NULL,
                claim_id TEXT NOT NULL,
                operation_id TEXT NOT NULL,
                kind TEXT NOT NULL,
                state TEXT NOT NULL DEFAULT 'completed',
                request TEXT NOT NULL,
                expected_revision INTEGER NOT NULL,
                receipt TEXT NOT NULL,
                created_at REAL NOT NULL,
                PRIMARY KEY(resource, claim_id, operation_id, kind)
            );
            CREATE TABLE IF NOT EXISTS releases (
                resource TEXT NOT NULL,
                claim_id TEXT NOT NULL,
                token TEXT NOT NULL,
                revision INTEGER NOT NULL,
                operation_id TEXT NOT NULL,
                request TEXT NOT NULL,
                released_at REAL NOT NULL,
                receipt TEXT NOT NULL,
                checkpoint TEXT,
                PRIMARY KEY(resource, claim_id)
            );
            """
        )
        operation_columns = {
            str(row["name"])
            for row in connection.execute("PRAGMA table_info(operations)")
        }
        if "state" not in operation_columns:
            connection.execute(
                "ALTER TABLE operations ADD COLUMN state "
                "TEXT NOT NULL DEFAULT 'completed'"
            )
        columns = {
            str(row["name"]) for row in connection.execute("PRAGMA table_info(claims)")
        }
        if "coordination_only" not in columns:
            connection.execute(
                "ALTER TABLE claims ADD COLUMN coordination_only "
                "INTEGER NOT NULL DEFAULT 0"
            )
        if "acquire_ttl" not in columns:
            connection.execute(
                "ALTER TABLE claims ADD COLUMN acquire_ttl REAL NOT NULL DEFAULT 900.0"
            )
        else:
            connection.execute(
                "UPDATE claims SET acquire_ttl = 900.0 WHERE acquire_ttl IS NULL"
            )
        if "checkpoint" not in columns:
            connection.execute("ALTER TABLE claims ADD COLUMN checkpoint TEXT")
        release_columns = {
            str(row["name"])
            for row in connection.execute("PRAGMA table_info(releases)")
        }
        if "checkpoint" not in release_columns:
            connection.execute("ALTER TABLE releases ADD COLUMN checkpoint TEXT")


def connect(home: str | os.PathLike[str] | None = None) -> sqlite3.Connection:
    """Open a configured connection and create/migrate the schema."""

    resolved_home = secure_directory(lease_home(home))
    database = resolved_home / "leases.sqlite3"
    sidecars = (
        database.with_name(f"{database.name}-wal"),
        database.with_name(f"{database.name}-shm"),
    )
    os.close(open_private_file(database))
    for state_file in sidecars:
        if state_file.exists() or state_file.is_symlink():
            os.close(open_private_file(state_file))
    connection = sqlite3.connect(database, timeout=30, isolation_level=None)
    connection.row_factory = sqlite3.Row
    connection.execute("PRAGMA busy_timeout = 30000")
    _schema(connection, resolved_home)
    for state_file in (database, *sidecars):
        if state_file.exists() or state_file.is_symlink():
            os.close(open_private_file(state_file))
    return connection


@contextmanager
def transaction(connection: sqlite3.Connection) -> Iterator[None]:
    """Run one immediate SQLite transaction."""

    connection.execute("BEGIN IMMEDIATE")
    try:
        yield
    except BaseException:
        connection.rollback()
        raise
    else:
        connection.commit()
