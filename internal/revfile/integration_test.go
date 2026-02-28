//go:build integration

package revfile

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const revFixture = "testdata/rev00000.dat"

func TestParseRealRevFile(t *testing.T) {
	data, err := os.ReadFile(revFixture)
	if os.IsNotExist(err) {
		t.Skip("rev00000.dat fixture not provided")
	}
	require.NoError(t, err)

	records, err := ParseAllRecords(bytes.NewReader(data))
	require.NoError(t, err)
	require.Greater(t, len(records), 0, "expected at least one record")

	// For each record, re-serialize BlockUndo and compare to RawUndo
	for i, rec := range records {
		var buf bytes.Buffer
		require.NoError(t, SerializeBlockUndo(&buf, rec.BlockUndo), "record %d: re-serialize", i)
		assert.Equal(t, rec.RawUndo, buf.Bytes(),
			"record %d: re-serialized bytes differ from original (len original=%d, len got=%d)",
			i, len(rec.RawUndo), buf.Len())
	}
}

func TestRecordCount(t *testing.T) {
	data, err := os.ReadFile(revFixture)
	if os.IsNotExist(err) {
		t.Skip("rev00000.dat fixture not provided")
	}
	require.NoError(t, err)

	records, err := ParseAllRecords(bytes.NewReader(data))
	require.NoError(t, err)
	t.Logf("rev00000.dat: %d records parsed", len(records))
	assert.Greater(t, len(records), 0)
}

func TestMagicBytes(t *testing.T) {
	data, err := os.ReadFile(revFixture)
	if os.IsNotExist(err) {
		t.Skip("rev00000.dat fixture not provided")
	}
	require.NoError(t, err)

	records, err := ParseAllRecords(bytes.NewReader(data))
	require.NoError(t, err)
	for i, rec := range records {
		assert.Equal(t, MainnetMagic, rec.Magic, "record %d has wrong magic", i)
	}
}

func TestUndoDataStructure(t *testing.T) {
	data, err := os.ReadFile(revFixture)
	if os.IsNotExist(err) {
		t.Skip("rev00000.dat fixture not provided")
	}
	require.NoError(t, err)

	records, err := ParseAllRecords(bytes.NewReader(data))
	require.NoError(t, err)

	const maxBTCSatoshis = int64(21_000_000 * 100_000_000)
	for i, rec := range records {
		for j, txundo := range rec.BlockUndo.TxUndos {
			for k, coin := range txundo.Prevouts {
				assert.Less(t, coin.Height, uint32(1_000_000),
					"record %d txundo %d coin %d: height %d unreasonably high", i, j, k, coin.Height)
				assert.GreaterOrEqual(t, coin.Out.Value, int64(0),
					"record %d txundo %d coin %d: negative value", i, j, k)
				assert.LessOrEqual(t, coin.Out.Value, maxBTCSatoshis,
					"record %d txundo %d coin %d: value exceeds max supply", i, j, k)
			}
		}
	}
}
