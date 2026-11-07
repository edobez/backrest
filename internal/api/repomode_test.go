package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/cryptoutil"
	"github.com/garethgeorge/backrest/internal/orchestrator/repo"
	"github.com/garethgeorge/backrest/internal/resticinstaller"
	"github.com/garethgeorge/backrest/internal/testutil"
)

func TestReadOnlyRepoRPCs(t *testing.T) {
	testutil.InstallZapLogger(t)

	// READ_ONLY never initializes a repo, so create it first.
	uri := t.TempDir()
	resticBin, err := resticinstaller.FindOrInstallResticBinary()
	if err != nil {
		t.Fatalf("find restic: %v", err)
	}
	managed := &v1.Repo{Id: "ro", Uri: uri, Password: "test", Flags: []string{"--no-cache"}}
	rorch, err := repo.NewRepoOrchestrator(&v1.Config{Instance: "test"}, managed, resticBin)
	if err != nil {
		t.Fatalf("create repo orchestrator: %v", err)
	}
	if err := rorch.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}

	sut := createSystemUnderTest(t, createConfigManager(&v1.Config{
		Version:  4,
		Modno:    1,
		Instance: "test",
		Repos: []*v1.Repo{
			{
				Id:       "ro",
				Guid:     cryptoutil.MustRandomID(cryptoutil.DefaultIDBits),
				Uri:      uri,
				Password: "test",
				Flags:    []string{"--no-cache"},
				Mode:     v1.RepoMode_REPO_MODE_READ_ONLY,
			},
		},
	}))
	ctx := context.Background()

	for _, task := range []v1.DoRepoTaskRequest_Task{
		v1.DoRepoTaskRequest_TASK_PRUNE,
		v1.DoRepoTaskRequest_TASK_FORGET,
		v1.DoRepoTaskRequest_TASK_UNLOCK,
	} {
		_, err := sut.handler.DoRepoTask(ctx, connect.NewRequest(&v1.DoRepoTaskRequest{RepoId: "ro", Task: task}))
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("DoRepoTask(%v): want FailedPrecondition, got %v", task, err)
		}
	}

	_, err = sut.handler.Forget(ctx, connect.NewRequest(&v1.ForgetRequest{RepoId: "ro", PlanId: "plan", SnapshotId: "abc"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("Forget: want FailedPrecondition, got %v", err)
	}

	_, err = sut.handler.PreviewAdoptedSource(ctx, connect.NewRequest(&v1.PreviewAdoptedSourceRequest{
		RepoId: "ro",
		Rule: &v1.AdoptedSource{
			Hostname:  "nas",
			Path:      "/data",
			Retention: &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 1}},
		},
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("PreviewAdoptedSource: want FailedPrecondition, got %v", err)
	}

	res, err := sut.handler.DoRepoTask(ctx, connect.NewRequest(&v1.DoRepoTaskRequest{RepoId: "ro", Task: v1.DoRepoTaskRequest_TASK_CHECK}))
	if err != nil {
		t.Fatalf("manual check: %v", err)
	}
	if res.Msg.OperationId == 0 {
		t.Errorf("manual check: no operation scheduled")
	}
}
