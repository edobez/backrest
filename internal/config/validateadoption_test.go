package config

import (
	"strings"
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
)

func TestValidateAdoptedSources(t *testing.T) {
	validGUID := strings.Repeat("a", 64)
	daily := &v1.Schedule{Schedule: &v1.Schedule_MaxFrequencyDays{MaxFrequencyDays: 1}}
	disabled := &v1.Schedule{Schedule: &v1.Schedule_Disabled{Disabled: true}}
	keepLast := &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 5}}
	keepAll := &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepAll{PolicyKeepAll: true}}
	rule := func(host, path string, retention *v1.RetentionPolicy) *v1.AdoptedSource {
		return &v1.AdoptedSource{Hostname: host, Path: path, Retention: retention, Schedule: daily}
	}

	tests := []struct {
		name    string
		mod     func(r *v1.Repo)
		wantErr []string // substrings; empty means valid
	}{
		{
			name: "valid rule",
			mod:  func(r *v1.Repo) { r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast)} },
		},
		{
			name: "valid rule with disabled repo forget policy",
			mod: func(r *v1.Repo) {
				r.ForgetPolicy = &v1.ForgetPolicy{Schedule: disabled, Retention: keepLast}
				r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast)}
			},
		},
		{
			name: "enabled repo forget policy",
			mod: func(r *v1.Repo) {
				r.ForgetPolicy = &v1.ForgetPolicy{Schedule: daily, Retention: keepLast}
				r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast)}
			},
			wantErr: []string{"cannot be combined with an enabled repo forget policy"},
		},
		{
			name: "append only repo",
			mod: func(r *v1.Repo) {
				r.Mode = v1.RepoMode_REPO_MODE_APPEND_ONLY
				r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast)}
			},
			wantErr: []string{"APPEND_ONLY", "adopted sources"},
		},
		{
			name: "read only repo",
			mod: func(r *v1.Repo) {
				r.Mode = v1.RepoMode_REPO_MODE_READ_ONLY
				r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast)}
			},
			wantErr: []string{"READ_ONLY", "adopted sources"},
		},
		{
			name:    "missing selector",
			mod:     func(r *v1.Repo) { r.AdoptedSources = []*v1.AdoptedSource{rule("", "", keepLast)} },
			wantErr: []string{"hostname is required", "path is required"},
		},
		{
			name:    "missing retention",
			mod:     func(r *v1.Repo) { r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", nil)} },
			wantErr: []string{"retention policy is required"},
		},
		{
			name:    "keep all retention",
			mod:     func(r *v1.Repo) { r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepAll)} },
			wantErr: []string{"keeps every snapshot"},
		},
		{
			name: "duplicate selector",
			mod: func(r *v1.Repo) {
				r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast), rule("nas", "/data", keepLast)}
			},
			wantErr: []string{"duplicate rule"},
		},
		{
			name: "same path on two hosts",
			mod: func(r *v1.Repo) {
				r.AdoptedSources = []*v1.AdoptedSource{rule("nas", "/data", keepLast), rule("laptop", "/data", keepLast)}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &v1.Repo{Id: "repo1", Uri: "/tmp/repo", Guid: validGUID}
			tc.mod(r)
			err := ValidateConfig(&v1.Config{Instance: "test", Repos: []*v1.Repo{r}})
			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %v, got nil", tc.wantErr)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
