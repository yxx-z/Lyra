// internal/lyrics/embedded.go
package lyrics

import (
	"context"
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// EmbeddedProvider reads lyrics embedded in the audio file's tags (USLT/LYRICS).
// It is purely local and makes no network calls.
type EmbeddedProvider struct{}

// NewEmbeddedProvider creates an EmbeddedProvider.
func NewEmbeddedProvider() *EmbeddedProvider { return &EmbeddedProvider{} }

// Name implements Provider.
func (p *EmbeddedProvider) Name() string { return "embedded" }

// Fetch reads embedded lyrics from q.FilePath. Returns ErrNotFound when the file
// is missing/unreadable or carries no embedded lyrics.
func (p *EmbeddedProvider) Fetch(_ context.Context, q Query) (Result, error) {
	if strings.TrimSpace(q.FilePath) == "" {
		return Result{}, ErrNotFound
	}
	f, err := os.Open(q.FilePath)
	if err != nil {
		return Result{}, ErrNotFound
	}
	defer f.Close()

	meta, err := tag.ReadFrom(f)
	if err != nil {
		return Result{}, ErrNotFound
	}
	content := strings.TrimSpace(meta.Lyrics())
	if content == "" {
		return Result{}, ErrNotFound
	}
	return Result{
		LRCContent:   content,
		PlainContent: content,
		Source:       "embedded",
	}, nil
}
