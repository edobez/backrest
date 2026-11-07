package tasks

import (
	"context"
	"io"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/pkg/restic"
)

// fakeRepoOrchestrator is a test double for the RepoOrchestrator interface.
// Each method returns the corresponding configured result/error fields.
type fakeRepoOrchestrator struct {
	unlockErr error

	backupResult *restic.BackupProgressEntry
	backupErr    error

	forgetResult []*v1.ResticSnapshot
	forgetErr    error

	forgetSnapshotErr error

	forgetDryRunGroups []restic.ForgetGroup
	forgetDryRunErr    error
	forgetSnapshotsErr error
	forgotSnapshotIDs  []string

	pruneErr error
	checkErr error

	statsResult *v1.RepoStats
	statsErr    error

	restoreResult *v1.RestoreProgressEntry
	restoreErr    error

	snapshots    []*restic.Snapshot
	snapshotsErr error

	addTagsErr error

	runCommandErr error

	// calls records the names of the methods called, in order.
	calls []string
}

var _ RepoOrchestrator = &fakeRepoOrchestrator{}

func (f *fakeRepoOrchestrator) UnlockIfAutoEnabled(ctx context.Context) error {
	f.calls = append(f.calls, "UnlockIfAutoEnabled")
	return f.unlockErr
}

func (f *fakeRepoOrchestrator) Backup(ctx context.Context, plan *v1.Plan, dryRun bool, cb func(event *restic.BackupProgressEntry)) (*restic.BackupProgressEntry, error) {
	f.calls = append(f.calls, "Backup")
	if cb != nil && f.backupResult != nil {
		cb(f.backupResult)
	}
	return f.backupResult, f.backupErr
}

func (f *fakeRepoOrchestrator) Forget(ctx context.Context, policy *v1.RetentionPolicy, opts ...restic.GenericOption) ([]*v1.ResticSnapshot, error) {
	f.calls = append(f.calls, "Forget")
	return f.forgetResult, f.forgetErr
}

func (f *fakeRepoOrchestrator) ForgetSnapshot(ctx context.Context, snapshotId string) error {
	f.calls = append(f.calls, "ForgetSnapshot")
	return f.forgetSnapshotErr
}

func (f *fakeRepoOrchestrator) ForgetDryRunBySource(ctx context.Context, policy *v1.RetentionPolicy, hostname, path string) ([]restic.ForgetGroup, error) {
	f.calls = append(f.calls, "ForgetDryRunBySource")
	return f.forgetDryRunGroups, f.forgetDryRunErr
}

func (f *fakeRepoOrchestrator) ForgetSnapshots(ctx context.Context, snapshotIDs []string) error {
	f.calls = append(f.calls, "ForgetSnapshots")
	if f.forgetSnapshotsErr == nil {
		f.forgotSnapshotIDs = append(f.forgotSnapshotIDs, snapshotIDs...)
	}
	return f.forgetSnapshotsErr
}

func (f *fakeRepoOrchestrator) Prune(ctx context.Context, output io.Writer) error {
	f.calls = append(f.calls, "Prune")
	return f.pruneErr
}

func (f *fakeRepoOrchestrator) Check(ctx context.Context, output io.Writer) error {
	f.calls = append(f.calls, "Check")
	return f.checkErr
}

func (f *fakeRepoOrchestrator) Stats(ctx context.Context) (*v1.RepoStats, error) {
	return f.statsResult, f.statsErr
}

func (f *fakeRepoOrchestrator) Restore(ctx context.Context, snapshotId string, snapshotPath string, target string, cb func(event *v1.RestoreProgressEntry)) (*v1.RestoreProgressEntry, error) {
	if cb != nil && f.restoreResult != nil {
		cb(f.restoreResult)
	}
	return f.restoreResult, f.restoreErr
}

func (f *fakeRepoOrchestrator) Snapshots(ctx context.Context) ([]*restic.Snapshot, error) {
	return f.snapshots, f.snapshotsErr
}

func (f *fakeRepoOrchestrator) AddTags(ctx context.Context, snapshotIDs []string, tags []string) error {
	f.calls = append(f.calls, "AddTags")
	return f.addTagsErr
}

func (f *fakeRepoOrchestrator) RunCommand(ctx context.Context, command string, writer io.Writer) error {
	return f.runCommandErr
}
