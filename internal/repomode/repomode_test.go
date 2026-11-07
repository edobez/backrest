package repomode

import (
	"errors"
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
)

func TestAllows(t *testing.T) {
	all := []Action{ActionBackup, ActionForget, ActionPrune, ActionUnlock, ActionInit, ActionScheduledCheck, ActionManualCheck, ActionTag}
	allowed := map[v1.RepoMode][]Action{
		v1.RepoMode_REPO_MODE_MANAGED:     all,
		v1.RepoMode_REPO_MODE_APPEND_ONLY: {ActionBackup, ActionInit, ActionScheduledCheck, ActionManualCheck},
		v1.RepoMode_REPO_MODE_READ_ONLY:   {ActionManualCheck},
	}
	for mode, want := range allowed {
		for _, a := range all {
			wantAllowed := false
			for _, w := range want {
				wantAllowed = wantAllowed || w == a
			}
			if got := Allows(mode, a); got != wantAllowed {
				t.Errorf("Allows(%v, %v) = %v, want %v", mode, a, got, wantAllowed)
			}
		}
	}
}

func TestCheck(t *testing.T) {
	if err := Check(nil, ActionPrune); err != nil {
		t.Errorf("nil repo must be MANAGED, got %v", err)
	}
	err := Check(&v1.Repo{Id: "r", Mode: v1.RepoMode_REPO_MODE_READ_ONLY}, ActionBackup)
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("want ErrNotAllowed, got %v", err)
	}
	if want := `backup on repo "r": not allowed by repo mode READ_ONLY`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if !NoLock(&v1.Repo{Mode: v1.RepoMode_REPO_MODE_READ_ONLY}) || NoLock(&v1.Repo{Mode: v1.RepoMode_REPO_MODE_APPEND_ONLY}) {
		t.Error("NoLock must hold for READ_ONLY only")
	}
}
