package repo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/ioutil"
	"github.com/garethgeorge/backrest/internal/repomode"
	"github.com/garethgeorge/backrest/pkg/restic"
	"github.com/garethgeorge/backrest/test/helpers"
)

// commandRecorder captures the restic command lines that the restic package
// logs ("command: ...") to the context logger.
type commandRecorder struct {
	buf bytes.Buffer
	w   ioutil.SynchronizedWriter
}

func newCommandRecorder() *commandRecorder {
	r := &commandRecorder{}
	r.w.W = &r.buf
	return r
}

func (r *commandRecorder) ctx() context.Context {
	return restic.ContextWithLogger(context.Background(), &r.w)
}

// commands returns the recorded command lines, then clears the recorder.
func (r *commandRecorder) commands() []string {
	r.w.Mu.Lock()
	defer r.w.Mu.Unlock()
	var cmds []string
	for _, line := range strings.Split(r.buf.String(), "\n") {
		if c, ok := strings.CutPrefix(line, "command: "); ok {
			cmds = append(cmds, c)
		}
	}
	r.buf.Reset()
	return cmds
}

// setupRepoWithSnapshot creates a MANAGED repo with one snapshot and returns
// its URI and the snapshot ID.
func setupRepoWithSnapshot(t *testing.T) (uri string, snapshotID string, testData string) {
	t.Helper()
	testData = helpers.CreateTestData(t)
	uri = t.TempDir()
	managed := initRepoHelper(t, configForTest, &v1.Repo{
		Id:       "test",
		Uri:      uri,
		Password: "test",
		Flags:    []string{"--no-cache"},
	})
	summary, err := managed.Backup(context.Background(), &v1.Plan{Id: "plan", Repo: "test", Paths: []string{testData}}, false, nil)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	return uri, summary.SnapshotId, testData
}

func TestReadOnlyRepoCommands(t *testing.T) {
	t.Parallel()

	uri, snapshotID, testData := setupRepoWithSnapshot(t)
	r, err := NewRepoOrchestrator(configForTest, &v1.Repo{
		Id:       "test",
		Uri:      uri,
		Password: "test",
		Flags:    []string{"--no-cache"},
		Mode:     v1.RepoMode_REPO_MODE_READ_ONLY,
	}, helpers.ResticBinary(t))
	if err != nil {
		t.Fatalf("create orchestrator: %v", err)
	}

	rec := newCommandRecorder()
	ctx := rec.ctx()

	// Allowed operations: each must run every restic command with --no-lock.
	allowed := map[string]func() error{
		"init (exists check)": func() error { return r.Init(ctx) },
		"snapshots":           func() error { _, err := r.Snapshots(ctx); return err },
		"ls":                  func() error { _, err := r.ListSnapshotFiles(ctx, snapshotID, testData); return err },
		"stats":               func() error { _, err := r.Stats(ctx); return err },
		"restore": func() error {
			_, err := r.Restore(ctx, snapshotID, path.Join(testData, "file10"), t.TempDir(), nil)
			return err
		},
		"dump": func() error { return r.Dump(ctx, snapshotID, path.Join(testData, "file10"), io.Discard) },
	}
	for name, fn := range allowed {
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cmds := rec.commands()
		if len(cmds) == 0 {
			t.Fatalf("%s: no restic command recorded", name)
		}
		for _, c := range cmds {
			if !strings.Contains(c, " --no-lock") {
				t.Errorf("%s: command missing --no-lock: %s", name, c)
			}
		}
	}

	// Forbidden operations: refused before any restic command runs.
	forbidden := map[string]func() error{
		"backup": func() error {
			_, err := r.Backup(ctx, &v1.Plan{Id: "plan", Repo: "test", Paths: []string{testData}}, false, nil)
			return err
		},
		"forget": func() error {
			_, err := r.Forget(ctx, &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 1}})
			return err
		},
		"forget snapshot": func() error { return r.ForgetSnapshot(ctx, snapshotID) },
		"prune":           func() error { return r.Prune(ctx, io.Discard) },
		"unlock":          func() error { return r.Unlock(ctx) },
		"tag":             func() error { return r.AddTags(ctx, []string{snapshotID}, []string{"x"}) },
	}
	for name, fn := range forbidden {
		if err := fn(); !errors.Is(err, repomode.ErrNotAllowed) {
			t.Errorf("%s: want ErrNotAllowed, got %v", name, err)
		}
		if cmds := rec.commands(); len(cmds) != 0 {
			t.Errorf("%s: ran restic commands: %v", name, cmds)
		}
	}

	// The manual check is the one command that takes a lock.
	if err := r.Check(ctx, io.Discard); err != nil {
		t.Fatalf("check: %v", err)
	}
	cmds := rec.commands()
	if len(cmds) != 1 || !strings.Contains(cmds[0], " check") {
		t.Fatalf("check: want one check command, got %v", cmds)
	}
	if strings.Contains(cmds[0], "--no-lock") {
		t.Errorf("check must take a lock, got: %s", cmds[0])
	}
}

func TestReadOnlyRepoNotInitialized(t *testing.T) {
	t.Parallel()

	uri := t.TempDir()
	r, err := NewRepoOrchestrator(configForTest, &v1.Repo{
		Id:       "test",
		Uri:      uri,
		Password: "test",
		Flags:    []string{"--no-cache"},
		Mode:     v1.RepoMode_REPO_MODE_READ_ONLY,
	}, helpers.ResticBinary(t))
	if err != nil {
		t.Fatalf("create orchestrator: %v", err)
	}

	if err := r.Init(context.Background()); !errors.Is(err, repomode.ErrNotAllowed) {
		t.Fatalf("init: want ErrNotAllowed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(uri, "config")); !os.IsNotExist(err) {
		t.Fatalf("init on READ_ONLY created a repo (stat config: %v)", err)
	}
}

func TestAppendOnlyRepo(t *testing.T) {
	t.Parallel()

	uri, snapshotID, testData := setupRepoWithSnapshot(t)
	r, err := NewRepoOrchestrator(configForTest, &v1.Repo{
		Id:         "test",
		Uri:        uri,
		Password:   "test",
		Flags:      []string{"--no-cache"},
		Mode:       v1.RepoMode_REPO_MODE_APPEND_ONLY,
		AutoUnlock: true,
	}, helpers.ResticBinary(t))
	if err != nil {
		t.Fatalf("create orchestrator: %v", err)
	}
	rec := newCommandRecorder()
	ctx := rec.ctx()

	if _, err := r.Backup(ctx, &v1.Plan{Id: "plan", Repo: "test", Paths: []string{testData}}, false, nil); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := r.Check(ctx, io.Discard); err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, c := range rec.commands() {
		if strings.Contains(c, "--no-lock") {
			t.Errorf("APPEND_ONLY must not add --no-lock: %s", c)
		}
	}

	if _, err := r.Forget(ctx, &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 1}}); !errors.Is(err, repomode.ErrNotAllowed) {
		t.Errorf("forget: want ErrNotAllowed, got %v", err)
	}
	if err := r.ForgetSnapshot(ctx, snapshotID); !errors.Is(err, repomode.ErrNotAllowed) {
		t.Errorf("forget snapshot: want ErrNotAllowed, got %v", err)
	}
	if err := r.Prune(ctx, io.Discard); !errors.Is(err, repomode.ErrNotAllowed) {
		t.Errorf("prune: want ErrNotAllowed, got %v", err)
	}
	if err := r.Unlock(ctx); !errors.Is(err, repomode.ErrNotAllowed) {
		t.Errorf("unlock: want ErrNotAllowed, got %v", err)
	}
	// auto_unlock is set (validation would reject it) but must be a no-op.
	if err := r.UnlockIfAutoEnabled(ctx); err != nil {
		t.Errorf("auto unlock: %v", err)
	}
	if cmds := rec.commands(); len(cmds) != 0 {
		t.Errorf("forbidden operations ran restic commands: %v", cmds)
	}
}
