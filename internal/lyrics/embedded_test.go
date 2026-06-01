// internal/lyrics/embedded_test.go
package lyrics

import (
	"context"
	"errors"
	"testing"
)

func TestEmbeddedProvider_Name(t *testing.T) {
	p := NewEmbeddedProvider()
	if p.Name() != "embedded" {
		t.Errorf("Name = %q, want embedded", p.Name())
	}
}

func TestEmbeddedProvider_MissingFile_NotFound(t *testing.T) {
	p := NewEmbeddedProvider()
	_, err := p.Fetch(context.Background(), Query{FilePath: "/nonexistent/file.mp3"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestEmbeddedProvider_EmptyPath_NotFound(t *testing.T) {
	p := NewEmbeddedProvider()
	_, err := p.Fetch(context.Background(), Query{FilePath: ""})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
