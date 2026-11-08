// Package cloudsync syncs the app's connections and settings through
// storage the user already has (a GitHub gist, a WebDAV folder, an
// S3-compatible bucket): one file there, encrypted on the device with a
// password only the user knows, merged three ways with the local copy and
// the last one synced.
package cloudsync

import (
	"context"
	"errors"
)

// FileName is the name of the file kept on the remote.
const FileName = "finallyshell-sync.json"

// Errors a Remote returns, which the app tells the user apart.
var (
	// ErrNotFound: the remote has no file yet.
	ErrNotFound = errors.New("cloudsync: no file on the remote")
	// ErrConflict: the file changed since it was read (a conditional
	// write failed); read it again and merge.
	ErrConflict = errors.New("cloudsync: the file changed on the remote")
	// ErrAuth: the remote refused the credentials (401, 403).
	ErrAuth = errors.New("cloudsync: the remote refused the credentials")
)

// Version identifies what a Get read, for the Put that replaces it.
type Version struct {
	// Tag is the provider's version of the file: an ETag, or "" when the
	// provider gave none, and the write cannot be conditional.
	Tag string
}

// Remote is one place the file is kept.
type Remote interface {
	// Get reads the file: ErrNotFound when there is none.
	Get(ctx context.Context) (data []byte, v Version, err error)
	// Put writes the file. prev is what Get read, nil when it found
	// none. When prev has a Tag and the provider supports it, the write
	// is conditional on it, and fails with ErrConflict when the file
	// changed in between.
	Put(ctx context.Context, data []byte, prev *Version) (Version, error)
}
