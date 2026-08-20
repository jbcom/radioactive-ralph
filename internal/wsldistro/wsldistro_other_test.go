//go:build !windows

package wsldistro

import (
	"context"
	"testing"
)

func TestCheckIsNotApplicableOffWindows(t *testing.T) {
	st := Check(context.Background())
	if st.Applicable {
		t.Fatalf("expected Applicable=false off Windows, got %+v", st)
	}
}
