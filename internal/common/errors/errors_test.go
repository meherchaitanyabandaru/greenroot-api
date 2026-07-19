package errors

import (
	stderrors "errors"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{ErrInvalidInput, ErrUnauthorized, ErrForbidden, ErrNotFound, ErrConflict}
	for i := 0; i < len(sentinels); i++ {
		for j := i + 1; j < len(sentinels); j++ {
			if stderrors.Is(sentinels[i], sentinels[j]) {
				t.Errorf("sentinel[%d] and sentinel[%d] should be distinct", i, j)
			}
		}
	}
}

func TestSentinelsIdentityViaIs(t *testing.T) {
	if !stderrors.Is(ErrForbidden, ErrForbidden) {
		t.Error("ErrForbidden should match itself via errors.Is")
	}
	if !stderrors.Is(ErrNotFound, ErrNotFound) {
		t.Error("ErrNotFound should match itself via errors.Is")
	}
}

func TestWrappedSentinelMatchesOriginal(t *testing.T) {
	wrapped := stderrors.Join(ErrForbidden, stderrors.New("extra context"))
	if !stderrors.Is(wrapped, ErrForbidden) {
		t.Error("wrapped ErrForbidden should still match via errors.Is")
	}
}
