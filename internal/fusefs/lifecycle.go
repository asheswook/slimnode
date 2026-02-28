package fusefs

import (
	"context"
)

// Start mounts the FUSE filesystem and serves requests until ctx is cancelled.
func (f *FS) Start(ctx context.Context) error {
	server, err := Mount(f)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		_ = server.Unmount()
	}()

	server.Wait()
	return nil
}

// Stop unmounts the FUSE filesystem.
func (f *FS) Stop() error {
	if f.server != nil {
		return f.server.Unmount()
	}
	return nil
}

// FinalizationEvents returns a channel that receives filenames when a file
// reaches 128 MiB and transitions to LOCAL_FINALIZED.
func (f *FS) FinalizationEvents() <-chan string {
	return f.finCh
}
