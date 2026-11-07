package config

import (
	"strings"
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestValidateRepoMode(t *testing.T) {
	validGUID := strings.Repeat("a", 64)
	daily := func() *v1.Schedule {
		return &v1.Schedule{Schedule: &v1.Schedule_MaxFrequencyDays{MaxFrequencyDays: 1}}
	}
	disabled := func() *v1.Schedule {
		return &v1.Schedule{Schedule: &v1.Schedule_Disabled{Disabled: true}}
	}
	keepLast := &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 5}}
	keepAll := &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepAll{PolicyKeepAll: true}}

	repo := func(mode v1.RepoMode, mod func(r *v1.Repo)) *v1.Repo {
		r := &v1.Repo{Id: "repo1", Uri: "/tmp/repo", Guid: validGUID, Mode: mode}
		if mod != nil {
			mod(r)
		}
		return r
	}
	plan := func(retention *v1.RetentionPolicy) *v1.Plan {
		return &v1.Plan{Id: "plan1", Repo: "repo1", Paths: []string{"/data"}, Retention: retention}
	}

	tests := []struct {
		name string
		repo *v1.Repo
		plan *v1.Plan
		// wantErr lists substrings the error must contain; empty means valid.
		wantErr []string
	}{
		{
			name: "managed allows everything",
			repo: repo(v1.RepoMode_REPO_MODE_MANAGED, func(r *v1.Repo) {
				r.AutoUnlock = true
				r.PrunePolicy = &v1.PrunePolicy{Schedule: daily()}
				r.CheckPolicy = &v1.CheckPolicy{Schedule: daily()}
				r.ForgetPolicy = &v1.ForgetPolicy{Schedule: daily(), Retention: keepLast}
			}),
			plan: plan(keepLast),
		},
		{
			name: "append only with plan without retention and disabled schedules",
			repo: repo(v1.RepoMode_REPO_MODE_APPEND_ONLY, func(r *v1.Repo) {
				r.PrunePolicy = &v1.PrunePolicy{Schedule: disabled()}
				r.ForgetPolicy = &v1.ForgetPolicy{Schedule: disabled()}
				r.CheckPolicy = &v1.CheckPolicy{Schedule: daily()}
			}),
			plan: plan(nil),
		},
		{
			name: "append only allows keep all retention",
			repo: repo(v1.RepoMode_REPO_MODE_APPEND_ONLY, nil),
			plan: plan(keepAll),
		},
		{
			name: "append only lists every conflict",
			repo: repo(v1.RepoMode_REPO_MODE_APPEND_ONLY, func(r *v1.Repo) {
				r.AutoUnlock = true
				r.PrunePolicy = &v1.PrunePolicy{Schedule: daily()}
				r.ForgetPolicy = &v1.ForgetPolicy{Schedule: daily(), Retention: keepLast}
			}),
			plan:    plan(keepLast),
			wantErr: []string{"APPEND_ONLY", `retention policy of plan "plan1"`, "forget policy schedule", "prune policy schedule", "auto unlock"},
		},
		{
			name: "read only without plans or schedules",
			repo: repo(v1.RepoMode_REPO_MODE_READ_ONLY, func(r *v1.Repo) {
				r.CheckPolicy = &v1.CheckPolicy{Schedule: disabled()}
				r.IndexPolicy = &v1.IndexPolicy{Schedule: daily()}
			}),
		},
		{
			name:    "read only rejects any plan",
			repo:    repo(v1.RepoMode_REPO_MODE_READ_ONLY, nil),
			plan:    plan(nil),
			wantErr: []string{"READ_ONLY", `plan "plan1"`},
		},
		{
			name: "read only rejects scheduled check and auto initialize",
			repo: repo(v1.RepoMode_REPO_MODE_READ_ONLY, func(r *v1.Repo) {
				r.Guid = ""
				r.AutoInitialize = true
				r.CheckPolicy = &v1.CheckPolicy{Schedule: daily()}
			}),
			wantErr: []string{"check policy schedule", "auto initialize"},
		},
		{
			name:    "invalid mode",
			repo:    repo(v1.RepoMode(42), nil),
			wantErr: []string{"invalid mode"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &v1.Config{Instance: "test", Repos: []*v1.Repo{tc.repo}}
			if tc.plan != nil {
				cfg.Plans = []*v1.Plan{tc.plan}
			}
			err := ValidateConfig(cfg)
			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestRepoModeDefaultsToManaged(t *testing.T) {
	// A config written before the mode field existed.
	var repo v1.Repo
	if err := protojson.Unmarshal([]byte(`{"id":"repo1","uri":"/tmp/repo","guid":"`+strings.Repeat("a", 64)+`","autoUnlock":true}`), &repo); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if repo.GetMode() != v1.RepoMode_REPO_MODE_MANAGED {
		t.Fatalf("mode = %v, want MANAGED", repo.GetMode())
	}
	if err := ValidateConfig(&v1.Config{Instance: "test", Repos: []*v1.Repo{&repo}}); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
