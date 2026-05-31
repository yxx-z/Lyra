// internal/scanner/walker.go
package scanner

import (
	"context"
	"io/fs"
	"path/filepath"
)

// Walk recursively finds audio files under each root.
// The returned channel is closed when all roots are exhausted or ctx is cancelled.
func Walk(ctx context.Context, roots []string) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		for _, root := range roots {
			filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if !IsAudioFile(path) {
					return nil
				}
				select {
				case ch <- path:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			})
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return ch
}
