package store

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS file_states (
    filename     TEXT PRIMARY KEY,
    state        TEXT NOT NULL CHECK(state IN ('ACTIVE','LOCAL_FINALIZED','CACHED','REMOTE')),
    source       TEXT NOT NULL CHECK(source IN ('server','local')),
    size         INTEGER NOT NULL,
    sha256       TEXT,
    created_at   INTEGER NOT NULL,
    last_access  INTEGER,
    height_first INTEGER,
    height_last  INTEGER
);
CREATE INDEX IF NOT EXISTS idx_file_states_state ON file_states(state);
CREATE INDEX IF NOT EXISTS idx_file_states_last_access ON file_states(last_access);
`

type SQLiteStore struct {
	readDB  *sql.DB
	writeDB *sql.DB

	stmtGetFile          *sql.Stmt
	stmtUpdateLastAccess *sql.Stmt
}

func New(dbPath string) (*SQLiteStore, error) {
	writeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open writeDB: %w", err)
	}
	writeDB.SetMaxOpenConns(1)

	if err := applyPragmas(writeDB); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("store: writeDB pragmas: %w", err)
	}

	if _, err := writeDB.Exec(schema); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}

	readDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("store: open readDB: %w", err)
	}
	readDB.SetMaxOpenConns(runtime.NumCPU())

	if err := applyPragmas(readDB); err != nil {
		readDB.Close()
		writeDB.Close()
		return nil, fmt.Errorf("store: readDB pragmas: %w", err)
	}

	s := &SQLiteStore{readDB: readDB, writeDB: writeDB}

	s.stmtGetFile, err = readDB.Prepare(selectCols + ` WHERE filename = ?`)
	if err != nil {
		s.closeDBs()
		return nil, fmt.Errorf("store: prepare GetFile: %w", err)
	}

	s.stmtUpdateLastAccess, err = writeDB.Prepare(
		`UPDATE file_states SET last_access = ? WHERE filename = ?`)
	if err != nil {
		s.stmtGetFile.Close()
		s.closeDBs()
		return nil, fmt.Errorf("store: prepare UpdateLastAccess: %w", err)
	}

	return s, nil
}

func applyPragmas(db *sql.DB) error {
	for _, p := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
	} {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

func (s *SQLiteStore) closeDBs() {
	if s.readDB != nil {
		s.readDB.Close()
	}
	if s.writeDB != nil {
		s.writeDB.Close()
	}
}

func (s *SQLiteStore) Close() error {
	if s.stmtGetFile != nil {
		s.stmtGetFile.Close()
	}
	if s.stmtUpdateLastAccess != nil {
		s.stmtUpdateLastAccess.Close()
	}
	if s.writeDB != nil {
		s.writeDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		s.writeDB.Close()
	}
	if s.readDB != nil {
		s.readDB.Close()
	}
	return nil
}

const selectCols = `SELECT filename, state, source, size, sha256, created_at, last_access, height_first, height_last FROM file_states`

func toUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(unix int64) time.Time {
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

func scanEntry(row interface{ Scan(dest ...any) error }) (*FileEntry, error) {
	var (
		e          FileEntry
		sha256     sql.NullString
		lastAccess sql.NullInt64
		createdAt  int64
		hFirst     sql.NullInt64
		hLast      sql.NullInt64
	)
	if err := row.Scan(&e.Filename, &e.State, &e.Source, &e.Size,
		&sha256, &createdAt, &lastAccess, &hFirst, &hLast); err != nil {
		return nil, err
	}
	e.SHA256 = sha256.String
	e.CreatedAt = fromUnix(createdAt)
	if lastAccess.Valid {
		e.LastAccess = fromUnix(lastAccess.Int64)
	}
	if hFirst.Valid {
		e.HeightFirst = hFirst.Int64
	}
	if hLast.Valid {
		e.HeightLast = hLast.Int64
	}
	return &e, nil
}

func collectRows(rows *sql.Rows) ([]FileEntry, error) {
	var entries []FileEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *e)
	}
	return entries, rows.Err()
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullI64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func (s *SQLiteStore) GetFile(filename string) (*FileEntry, error) {
	e, err := scanEntry(s.stmtGetFile.QueryRow(filename))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("store: file not found: %s", filename)
	}
	if err != nil {
		return nil, fmt.Errorf("store: GetFile: %w", err)
	}
	return e, nil
}

func (s *SQLiteStore) ListFiles() ([]FileEntry, error) {
	rows, err := s.readDB.Query(selectCols)
	if err != nil {
		return nil, fmt.Errorf("store: ListFiles: %w", err)
	}
	defer rows.Close()
	return collectRows(rows)
}

func (s *SQLiteStore) ListByState(state FileState) ([]FileEntry, error) {
	rows, err := s.readDB.Query(selectCols+` WHERE state = ?`, string(state))
	if err != nil {
		return nil, fmt.Errorf("store: ListByState: %w", err)
	}
	defer rows.Close()
	return collectRows(rows)
}

func (s *SQLiteStore) ListCachedByLRU(limit int) ([]FileEntry, error) {
	rows, err := s.readDB.Query(
		selectCols+` WHERE state = 'CACHED' ORDER BY last_access ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: ListCachedByLRU: %w", err)
	}
	defer rows.Close()
	return collectRows(rows)
}

func (s *SQLiteStore) UpsertFile(entry *FileEntry) error {
	return s.execImmediate(`
		INSERT INTO file_states (filename, state, source, size, sha256, created_at, last_access, height_first, height_last)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(filename) DO UPDATE SET
			state        = excluded.state,
			source       = excluded.source,
			size         = excluded.size,
			sha256       = excluded.sha256,
			created_at   = excluded.created_at,
			last_access  = excluded.last_access,
			height_first = excluded.height_first,
			height_last  = excluded.height_last`,
		entry.Filename,
		string(entry.State),
		string(entry.Source),
		entry.Size,
		nullStr(entry.SHA256),
		toUnix(entry.CreatedAt),
		nullI64(toUnix(entry.LastAccess)),
		nullI64(entry.HeightFirst),
		nullI64(entry.HeightLast),
	)
}

func (s *SQLiteStore) UpdateState(filename string, state FileState) error {
	return s.execImmediate(`UPDATE file_states SET state = ? WHERE filename = ?`, string(state), filename)
}

func (s *SQLiteStore) UpdateLastAccess(filename string, t time.Time) error {
	return s.execImmediate(`UPDATE file_states SET last_access = ? WHERE filename = ?`, toUnix(t), filename)
}

func (s *SQLiteStore) DeleteFile(filename string) error {
	return s.execImmediate(`DELETE FROM file_states WHERE filename = ?`, filename)
}

func (s *SQLiteStore) execImmediate(query string, args ...any) error {
	ctx := context.Background()
	conn, err := s.writeDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("store: BEGIN IMMEDIATE: %w", err)
	}
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return fmt.Errorf("store: exec: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
