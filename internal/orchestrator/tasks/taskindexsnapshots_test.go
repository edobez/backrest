package tasks

import (
	"testing"
	"time"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/cryptoutil"
	"github.com/garethgeorge/backrest/internal/oplog"
	"github.com/garethgeorge/backrest/internal/oplog/sqlitestore"
)

func TestScheduledIndexSnapshotsNext(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	hourly := &v1.IndexPolicy{Schedule: &v1.Schedule{
		Schedule: &v1.Schedule_MaxFrequencyHours{MaxFrequencyHours: 1},
		Clock:    v1.Schedule_CLOCK_LAST_RUN_TIME,
	}}

	tests := []struct {
		name    string
		policy  *v1.IndexPolicy
		lastRun time.Time // zero = never ran
		want    time.Time // zero = never scheduled
	}{
		{name: "no policy", policy: nil},
		{name: "disabled", policy: &v1.IndexPolicy{Schedule: &v1.Schedule{Schedule: &v1.Schedule_Disabled{Disabled: true}}}},
		{name: "never ran runs now", policy: hourly, want: now},
		{name: "ran recently waits", policy: hourly, lastRun: now.Add(-15 * time.Minute), want: now.Add(45 * time.Minute)},
		{name: "overdue runs now", policy: hourly, lastRun: now.Add(-3 * time.Hour), want: now},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &v1.Repo{Id: "repo", Guid: cryptoutil.MustRandomID(cryptoutil.DefaultIDBits), IndexPolicy: tc.policy}
			if !tc.lastRun.IsZero() {
				recordIndexRun(repo.Guid, tc.lastRun)
			}

			opstore, err := sqlitestore.NewMemorySqliteStore(t)
			if err != nil {
				t.Fatalf("create opstore: %v", err)
			}
			log, err := oplog.NewOpLog(opstore)
			if err != nil {
				t.Fatalf("create oplog: %v", err)
			}
			runner := newTestTaskRunner(t, &v1.Config{Instance: "test", Repos: []*v1.Repo{repo}}, log)

			st, err := NewScheduledIndexSnapshotsTask(repo).Next(now, runner)
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if tc.want.IsZero() {
				if !st.Eq(NeverScheduledTask) {
					t.Errorf("expected never scheduled, got run at %v", st.RunAt)
				}
				return
			}
			if !st.RunAt.Equal(tc.want) {
				t.Errorf("run at = %v, want %v", st.RunAt, tc.want)
			}
			if st.Op != nil {
				t.Errorf("index task should not create an operation, got %v", st.Op)
			}
		})
	}
}
