package api

import (
	"context"

	"connectrpc.com/connect"
	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/version"
	"google.golang.org/protobuf/types/known/emptypb"
)

// GetVersion returns the version and commit of the running backend.
func (s *BackrestHandler) GetVersion(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[v1.VersionInfo], error) {
	info := version.Current()
	return connect.NewResponse(&v1.VersionInfo{
		Version:    info.Version,
		Commit:     info.Commit,
		IsDevBuild: info.IsDevBuild,
	}), nil
}
