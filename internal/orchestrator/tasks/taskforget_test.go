package tasks

import (
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
)

func TestGroupByFlag(t *testing.T) {
	tests := []struct {
		groupBy v1.ForgetPolicy_GroupBy
		want    string
	}{
		{v1.ForgetPolicy_GROUP_BY_TAGS, "tags"},
		{v1.ForgetPolicy_GROUP_BY_NONE, ""},
		{v1.ForgetPolicy_GROUP_BY_HOST, "host"},
		{v1.ForgetPolicy_GROUP_BY_HOST_PATHS, "host,paths"},
		{v1.ForgetPolicy_GROUP_BY_HOST_TAGS, "host,tags"},
		{v1.ForgetPolicy_GroupBy(99), "tags"},
	}
	for _, tc := range tests {
		t.Run(tc.groupBy.String(), func(t *testing.T) {
			if got := groupByFlag(tc.groupBy); got != tc.want {
				t.Errorf("groupByFlag(%v) = %q, want %q", tc.groupBy, got, tc.want)
			}
		})
	}
}
