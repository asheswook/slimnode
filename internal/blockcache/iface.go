package blockcache

type BlockCache interface {
	HasBlock(blkFile string, fileOffset int64) bool
	GetBlock(blkFile string, fileOffset int64) ([]byte, error)
	StoreBlock(blkFile string, fileOffset int64, data []byte) error
	RemoveFile(blkFile string) error
	Usage() (used int64, total int64)
}
