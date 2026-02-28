package revfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstants(t *testing.T) {
	// Test MainnetMagic
	assert.Equal(t, [4]byte{0xf9, 0xbe, 0xb4, 0xd9}, MainnetMagic)

	// Test StorageHeaderSize
	assert.Equal(t, 8, StorageHeaderSize)

	// Test ChecksumSize
	assert.Equal(t, 32, ChecksumSize)
}
