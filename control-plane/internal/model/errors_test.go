package model_test

import (
	"errors"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestErrNotFoundIsSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("lookup failed"), model.ErrNotFound)
	if !errors.Is(wrapped, model.ErrNotFound) {
		t.Fatal("ErrNotFound must be identifiable through wrapping")
	}
}
