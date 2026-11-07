package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/cryptoutil"
	"github.com/garethgeorge/backrest/internal/resticinstaller"
	"github.com/garethgeorge/backrest/internal/sources"
	"github.com/garethgeorge/backrest/internal/testutil"
)

// TestListSources covers a repo mixing a Backrest plan snapshot with snapshots created by plain
// `restic backup` from another host, including a multi-path one that is a superset of another source.
func TestListSources(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	planDir := t.TempDir()
	dataDir := t.TempDir()
	etcDir := t.TempDir()
	for _, d := range []string{planDir, dataDir, etcDir} {
		if err := os.WriteFile(filepath.Join(d, "file.txt"), []byte(d), 0644); err != nil {
			t.Fatal(err)
		}
	}

	sut := createSystemUnderTest(t, createConfigManager(&v1.Config{
		Version:  4,
		Modno:    1234,
		Instance: "test",
		Repos: []*v1.Repo{
			{
				Id:       "local",
				Guid:     cryptoutil.MustRandomID(cryptoutil.DefaultIDBits),
				Uri:      repoDir,
				Password: "test",
				Flags:    []string{"--no-cache"},
			},
		},
		Plans: []*v1.Plan{
			{
				Id:       "plan1",
				Repo:     "local",
				Paths:    []string{planDir},
				Schedule: &v1.Schedule{Schedule: &v1.Schedule_Disabled{Disabled: true}},
			},
		},
	}))

	ctx, cancel := testutil.WithDeadlineFromTest(t, context.Background())
	defer cancel()
	go sut.orch.Run(ctx)

	// External snapshots, as another tool on another machine would create them.
	resticBin, err := resticinstaller.FindOrInstallResticBinary()
	if err != nil {
		t.Fatalf("find restic: %v", err)
	}
	externalBackup := func(paths ...string) {
		args := append([]string{"backup", "--no-cache", "--host", "nas"}, paths...)
		cmd := exec.CommandContext(ctx, resticBin, args...)
		cmd.Env = append(os.Environ(), "RESTIC_REPOSITORY="+repoDir, "RESTIC_PASSWORD=test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("restic %v: %v\n%s", args, err, out)
		}
	}
	externalBackup(dataDir)
	externalBackup(dataDir)
	externalBackup(etcDir, dataDir)

	if _, err := sut.handler.Backup(ctx, connect.NewRequest(&v1.BackupRequest{Value: "plan1"})); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	planSource := sources.Key{Hostname: mustHostname(t), Paths: []string{planDir}}.ID()
	dataSource := sources.Key{Hostname: "nas", Paths: []string{dataDir}}.ID()
	bothPaths := []string{dataDir, etcDir}
	slices.Sort(bothPaths)
	bothSource := sources.Key{Hostname: "nas", Paths: bothPaths}.ID()

	// The backup triggers an index of the whole repo, which picks up the external snapshots too.
	var got []*v1.SnapshotSource
	if err := testutil.Retry(t, ctx, func() error {
		resp, err := sut.handler.ListSources(ctx, connect.NewRequest(&v1.ListSourcesRequest{RepoId: "local"}))
		if err != nil {
			return err
		}
		got = resp.Msg.Sources
		if len(got) != 3 {
			return fmt.Errorf("expected 3 sources, got %d: %v", len(got), got)
		}
		return nil
	}); err != nil {
		t.Fatalf("ListSources: %v", err)
	}

	byID := make(map[string]*v1.SnapshotSource)
	for _, s := range got {
		byID[s.Id] = s
	}
	wantCounts := map[string]struct {
		count int64
		plans []string
	}{
		planSource: {1, []string{"plan1"}},
		dataSource: {2, []string{}},
		bothSource: {1, []string{}},
	}
	for id, want := range wantCounts {
		s, ok := byID[id]
		if !ok {
			t.Errorf("source %s missing from %v", id, got)
			continue
		}
		if s.SnapshotCount != want.count {
			t.Errorf("source %v: snapshot count = %d, want %d", s.Paths, s.SnapshotCount, want.count)
		}
		if !slices.Equal(s.PlanIds, want.plans) {
			t.Errorf("source %v: plan IDs = %v, want %v", s.Paths, s.PlanIds, want.plans)
		}
		if s.LastSnapshotId == "" || s.LastSnapshotTimeMs < s.FirstSnapshotTimeMs {
			t.Errorf("source %v: bad last snapshot fields: %v", s.Paths, s)
		}
	}

	// The source filter must be exact: the [data, etc] source must not show up for the [data] source.
	resp, err := sut.handler.ListSnapshots(ctx, connect.NewRequest(&v1.ListSnapshotsRequest{RepoId: "local", SourceId: dataSource}))
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(resp.Msg.Snapshots) != 2 {
		t.Errorf("ListSnapshots(source=data) returned %d snapshots, want 2", len(resp.Msg.Snapshots))
	}
	for _, s := range resp.Msg.Snapshots {
		if !slices.Equal(s.Paths, []string{dataDir}) || s.Hostname != "nas" {
			t.Errorf("ListSnapshots(source=data) returned snapshot of another source: %v %v", s.Hostname, s.Paths)
		}
	}
}

func mustHostname(t *testing.T) string {
	h, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return h
}
