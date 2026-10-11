//go:build !darwin && !linux

package astcontext

import (
	"context"
	"os"
)

func openSnapshotDirectory(context.Context, string) (*snapshotDirectory, error) {
	return nil, errSnapshotPlatform
}
func (*snapshotDirectory) scanFile() (*os.File, error) { return nil, errSnapshotPlatform }
func (*snapshotDirectory) child(context.Context, string) (*snapshotDirectory, error) {
	return nil, errSnapshotPlatform
}
func (*snapshotDirectory) regular(context.Context, string) (*os.File, error) {
	return nil, errSnapshotPlatform
}
func (*snapshotDirectory) metadata(context.Context, string) (snapshotMetadata, error) {
	return snapshotMetadata{}, errSnapshotPlatform
}
