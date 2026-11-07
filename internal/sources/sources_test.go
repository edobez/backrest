package sources

import (
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
)

func snap(id, host string, timeMs int64, paths []string, tags ...string) *v1.ResticSnapshot {
	return &v1.ResticSnapshot{Id: id, Hostname: host, UnixTimeMs: timeMs, Paths: paths, Tags: tags}
}

func TestKeyMatchesResticGrouping(t *testing.T) {
	tests := []struct {
		name    string
		a, b    *v1.ResticSnapshot
		sameKey bool
	}{
		{
			name:    "path order does not matter",
			a:       snap("1", "nas", 0, []string{"/etc", "/data"}),
			b:       snap("2", "nas", 0, []string{"/data", "/etc"}),
			sameKey: true,
		},
		{
			name:    "tags are not part of the key",
			a:       snap("1", "nas", 0, []string{"/data"}, "plan:p1", "created-by:i1"),
			b:       snap("2", "nas", 0, []string{"/data"}, "daily"),
			sameKey: true,
		},
		{
			name:    "superset paths are a different source",
			a:       snap("1", "nas", 0, []string{"/data"}),
			b:       snap("2", "nas", 0, []string{"/data", "/etc"}),
			sameKey: false,
		},
		{
			name:    "hostname is case-sensitive",
			a:       snap("1", "nas", 0, []string{"/data"}),
			b:       snap("2", "NAS", 0, []string{"/data"}),
			sameKey: false,
		},
		{
			name:    "no path normalization beyond sorting",
			a:       snap("1", "nas", 0, []string{"/data"}),
			b:       snap("2", "nas", 0, []string{"/data/"}),
			sameKey: false,
		},
		{
			name:    "different hosts are different sources",
			a:       snap("1", "nas", 0, []string{"/data"}),
			b:       snap("2", "laptop", 0, []string{"/data"}),
			sameKey: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IDForSnapshot(tc.a) == IDForSnapshot(tc.b)
			if got != tc.sameKey {
				t.Errorf("same key = %v, want %v (keys %+v, %+v)", got, tc.sameKey, KeyForSnapshot(tc.a), KeyForSnapshot(tc.b))
			}
		})
	}
}

func TestKeyForSnapshotDoesNotMutate(t *testing.T) {
	s := snap("1", "nas", 0, []string{"/b", "/a"})
	KeyForSnapshot(s)
	if diff := cmp.Diff([]string{"/b", "/a"}, s.Paths); diff != "" {
		t.Errorf("snapshot paths were modified (-want +got):\n%s", diff)
	}
}

func TestIDIsStable(t *testing.T) {
	// Pin the ID so accidental changes to the key format, which would break bookmarked links, are noticed.
	got := Key{Hostname: "nas", Paths: []string{"/data"}}.ID()
	if len(got) != sourceIDLen {
		t.Fatalf("ID length = %d, want %d", len(got), sourceIDLen)
	}
	if again := (Key{Hostname: "nas", Paths: []string{"/data"}}).ID(); again != got {
		t.Errorf("ID not deterministic: %q != %q", got, again)
	}
	const want = "43defe6c5a23da48"
	if got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
}

func TestBuild(t *testing.T) {
	withSize := func(s *v1.ResticSnapshot, bytes int64) *v1.ResticSnapshot {
		s.Summary = &v1.SnapshotSummary{TotalBytesProcessed: bytes}
		return s
	}

	snapshots := []*v1.ResticSnapshot{
		// Backrest plan snapshots on nas:/data, plus an external one on the same source.
		snap("a1", "nas", 1000, []string{"/data"}, "plan:p1", "created-by:i1"),
		withSize(snap("a2", "nas", 3000, []string{"/data"}, "plan:p1", "created-by:i1"), 42),
		snap("a3", "nas", 2000, []string{"/data"}),
		// External multi-path source, paths in a different order.
		snap("b1", "nas", 1500, []string{"/etc", "/data"}),
		snap("b2", "nas", 2500, []string{"/data", "/etc"}, "daily"),
		// Another host.
		snap("c1", "laptop", 500, []string{"/home"}, "plan:p2", "created-by:i2"),
		// Duplicate (e.g. indexed by two instances) is counted once.
		snap("c1", "laptop", 500, []string{"/home"}, "plan:p2", "created-by:i2"),
	}

	got := Build("repo1", snapshots)

	want := []*v1.SnapshotSource{
		{
			Id:                  Key{Hostname: "laptop", Paths: []string{"/home"}}.ID(),
			RepoId:              "repo1",
			Hostname:            "laptop",
			Paths:               []string{"/home"},
			PlanIds:             []string{"p2"},
			InstanceIds:         []string{"i2"},
			SnapshotCount:       1,
			FirstSnapshotTimeMs: 500,
			LastSnapshotTimeMs:  500,
			LastSnapshotId:      "c1",
		},
		{
			Id:                  Key{Hostname: "nas", Paths: []string{"/data"}}.ID(),
			RepoId:              "repo1",
			Hostname:            "nas",
			Paths:               []string{"/data"},
			PlanIds:             []string{"p1"},
			InstanceIds:         []string{"i1"},
			SnapshotCount:       3,
			FirstSnapshotTimeMs: 1000,
			LastSnapshotTimeMs:  3000,
			LastSnapshotId:      "a2",
			LastSnapshotBytes:   42,
		},
		{
			Id:                  Key{Hostname: "nas", Paths: []string{"/data", "/etc"}}.ID(),
			RepoId:              "repo1",
			Hostname:            "nas",
			Paths:               []string{"/data", "/etc"},
			PlanIds:             []string{},
			InstanceIds:         []string{},
			SnapshotCount:       2,
			FirstSnapshotTimeMs: 1500,
			LastSnapshotTimeMs:  2500,
			LastSnapshotId:      "b2",
		},
	}

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("Build() mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildEmpty(t *testing.T) {
	if got := Build("repo1", nil); len(got) != 0 {
		t.Errorf("Build(nil) = %v, want empty", got)
	}
}
