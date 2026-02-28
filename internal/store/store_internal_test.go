package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_InvalidPath(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "nonexistent_dir", "test.db")
	s, err := New(badPath)
	require.Error(t, err)
	assert.Nil(t, s)
}

func TestCloseDBs_Basic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	require.NoError(t, err)
	s.closeDBs()
}

func TestCloseDBs_NilDBs(t *testing.T) {
	s := &SQLiteStore{}
	assert.NotPanics(t, func() { s.closeDBs() })
}

func TestClose_AfterClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.NoError(t, s.Close())
}

func TestExecImmediate_ClosedDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	require.NoError(t, err)
	s.stmtGetFile.Close()
	s.stmtUpdateLastAccess.Close()
	s.writeDB.Close()

	err = s.execImmediate(`SELECT 1`)
	assert.Error(t, err)
}

func TestExecImmediate_BadSQL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	require.NoError(t, err)
	defer s.Close()

	err = s.execImmediate(`THIS IS NOT VALID SQL !!!`)
	assert.Error(t, err)
}

func TestGetFile_NotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.GetFile("does_not_exist.dat")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListFiles_EmptyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	require.NoError(t, err)
	defer s.Close()

	files, err := s.ListFiles()
	require.NoError(t, err)
	assert.Len(t, files, 0)
}

func TestNullStr(t *testing.T) {
	ns := nullStr("")
	assert.False(t, ns.Valid)
	assert.Equal(t, "", ns.String)

	ns2 := nullStr("deadbeef")
	assert.True(t, ns2.Valid)
	assert.Equal(t, "deadbeef", ns2.String)
}

func TestFromUnix(t *testing.T) {
	z := fromUnix(0)
	assert.True(t, z.IsZero())

	ts := int64(1_700_000_000)
	got := fromUnix(ts)
	assert.Equal(t, time.Unix(ts, 0), got)
	assert.False(t, got.IsZero())
}
