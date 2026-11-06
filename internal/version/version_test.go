package version

import (
	"runtime/debug"
	"testing"
)

func bi(mainVersion string, kv ...string) *debug.BuildInfo {
	b := &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
	for i := 0; i+1 < len(kv); i += 2 {
		b.Settings = append(b.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return b
}

func TestResolve(t *testing.T) {
	rev := "148ba12abcdef0123456789abcdef0123456789a"
	tests := []struct {
		name         string
		ldVer, ldCom string
		bi           *debug.BuildInfo
		want         Info
	}{
		{"ldflags win", "1.9.2", "deadbeef", bi("(devel)", "vcs.revision", rev), Info{"1.9.2", "deadbeef", false}},
		{"no build info", "unknown", "unknown", nil, Info{"dev", "unknown", true}},
		{"vcs clean", "unknown", "unknown", bi("(devel)", "vcs.revision", rev, "vcs.modified", "false"), Info{"dev+148ba12", rev, true}},
		{"vcs dirty", "unknown", "unknown", bi("(devel)", "vcs.revision", rev, "vcs.modified", "true"), Info{"dev+148ba12-dirty", rev, true}},
		{"tag version", "unknown", "unknown", bi("v1.9.2", "vcs.revision", rev), Info{"v1.9.2-dev+148ba12", rev, true}},
		{"pseudo version ignored", "unknown", "unknown", bi("v1.9.3-0.20240101000000-abcdef123456", "vcs.revision", rev), Info{"dev+148ba12", rev, true}},
		{"empty ldflags", "", "", nil, Info{"dev", "unknown", true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.ldVer, tc.ldCom, tc.bi); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
