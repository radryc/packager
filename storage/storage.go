// Package storage provides a storage abstraction that decouples archive logic
// from the physical medium. Implementations translate ReadAt calls into local
// disk seeks or cloud-storage byte-range requests.
package storage

import "io"

// ObjectReader is the interface that archive readers use to fetch byte ranges
// from the underlying storage. Implementations may wrap a local file, an S3
// object, or any other byte-addressable store.
type ObjectReader interface {
	io.ReaderAt
	io.Closer
	// Size returns the total size in bytes of the stored object.
	Size() (int64, error)
}
