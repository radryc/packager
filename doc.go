// Package packager implements a cloud-native encrypted archive format
// using an "Indexed-Tail" layout optimised for O(1) random-access retrieval
// over byte-range-capable storage (local files, S3, GCS, …).
//
// Archive binary layout:
//
//	┌─────────────────────────────┐
//	│  Data Block 0               │  ← individually packed file data
//	│  Data Block 1               │
//	│  …                          │
//	│  Data Block N               │
//	├─────────────────────────────┤
//	│  Encrypted Master Index     │  ← JSON index, zstd + AEAD
//	├─────────────────────────────┤
//	│  8-byte Footer (LE uint64)  │  ← size of the encrypted index
//	└─────────────────────────────┘
package packager
