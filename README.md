# packager

A high-performance Go library that packs multiple files into a single encrypted, compressed archive optimised for cloud object storage (S3, GCS, local disk). It solves the "small file problem" by enabling **O(1) random-access** retrieval of individual files over the network without downloading or scanning the entire archive.

## Features

- **Indexed-Tail format** — sequential data blocks + encrypted master index + 8-byte footer. Retrieves any file in 2–3 byte-range requests.
- **Per-file encryption** — ChaCha20-Poly1305 (AEAD) with unique random nonces. Opt out per file or keep everything encrypted.
- **Ultra-fast compression** — zstd via [klauspost/compress](https://github.com/klauspost/compress). Multi-GB/s decompression, automatic skip for already-compressed formats.
- **Smart detection** — PNG, JPEG, GIF, ZIP, MP4, and 20+ other formats are auto-detected by extension and magic bytes; compression is skipped automatically.
- **Rich metadata** — each file entry stores Unix permissions, owner UID, encryption flag, and compression flag.
- **WORM model** — write-once, read-many. Archives are immutable; updates create new archives.
- **Pluggable storage** — `ObjectReader` interface with built-in implementations for local files, AWS S3, and Google Cloud Storage.
- **Bit-rot protection** — AEAD authentication detects any corruption or tampering on read.

## Archive Layout

```
┌─────────────────────────────┐
│  Data Block 0               │  ← individually packed file data
│  Data Block 1               │
│  …                          │
│  Data Block N               │
├─────────────────────────────┤
│  Encrypted Master Index     │  ← JSON index, zstd + ChaCha20-Poly1305
├─────────────────────────────┤
│  8-byte Footer (LE uint64)  │  ← size of the encrypted index
└─────────────────────────────┘
```

**Retrieval flow:**
1. Fetch the last 8 bytes → learn index size (skippable if cached).
2. Fetch the encrypted index → decrypt → get file map.
3. Fetch exact byte range for the target file → decrypt → decompress.

## Installation

```bash
go get github.com/radryc/packager
```

## Quick Start

### Writing an archive

```go
package main

import (
    "bytes"
    "crypto/rand"
    "log"

    "github.com/radryc/packager"
    "github.com/radryc/packager/pipeline"
)

func main() {
    // Generate a 32-byte encryption key
    key := make([]byte, 32)
    rand.Read(key)

    p, err := pipeline.NewPipeline(key)
    if err != nil {
        log.Fatal(err)
    }

    var buf bytes.Buffer
    w := packager.NewArchiveWriter(&buf, p)

    // Add files with metadata
    err = w.AddFile("src/main.go", []byte("package main\n"), packager.AddFileOptions{
        Permission: 0644,
        OwnerUID:   1000,
        Encrypt:    true,
    })
    if err != nil {
        log.Fatal(err)
    }

    // PNG auto-detected as pre-compressed → zstd skipped
    err = w.AddFile("logo.png", pngBytes, packager.AddFileOptions{
        Permission: 0444,
        OwnerUID:   1000,
        Encrypt:    true,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Finalise: writes encrypted index + footer
    if err := w.Close(); err != nil {
        log.Fatal(err)
    }
}
```

### Reading an archive

```go
package main

import (
    "fmt"
    "log"

    "github.com/radryc/packager"
    "github.com/radryc/packager/pipeline"
    "github.com/radryc/packager/storage"
)

func main() {
    key := []byte{ /* same 32 bytes used during write */ }

    p, _ := pipeline.NewPipeline(key)
    store, _ := storage.NewLocalFileReader("archive.pack")

    ar, err := packager.OpenArchive(store, p)
    if err != nil {
        log.Fatal(err)
    }
    defer ar.Close()

    // List all files
    for _, path := range ar.ListFiles() {
        fmt.Println(path)
    }

    // Retrieve a single file (O(1) lookup + byte-range read)
    data, entry, err := ar.GetFile("src/main.go")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Data: %s\n", data)
    fmt.Printf("Perms: %o, Owner: %d, Encrypted: %v, Compressed: %v\n",
        entry.Permission, entry.OwnerUID, entry.IsEncrypted, entry.IsCompressed)
}
```

### Using with S3

```go
import (
    "context"

    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/radryc/packager/storage"
)

cfg, _ := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
client := s3.NewFromConfig(cfg)

store := storage.NewS3Reader(client, "my-bucket", "archives/repo.pack",
    storage.WithS3Context(ctx),
)

ar, err := packager.OpenArchive(store, p,
    packager.WithIndexSize(cachedSize), // skip footer fetch if cached
)
```

### Using with GCS

```go
import (
    gcStorage "cloud.google.com/go/storage"
    "github.com/radryc/packager/storage"
)

gcsClient, _ := gcStorage.NewClient(ctx)
bucket := gcsClient.Bucket("my-bucket")

store := storage.NewGCSReader(bucket, "archives/repo.pack",
    storage.WithGCSContext(ctx),
)

ar, err := packager.OpenArchive(store, p)
```

## File Entry Metadata

Each file in the archive carries:

| Field | JSON Key | Type | Description |
|---|---|---|---|
| Offset | `o` | int64 | Byte offset of the packed data block |
| Size | `s` | int64 | Byte length of the packed data block |
| Permission | `p` | uint32 | Unix permission bits (e.g. 0644) |
| OwnerUID | `u` | int | Numeric user ID of the file owner |
| IsEncrypted | `e` | bool | Whether AEAD encryption was applied |
| IsCompressed | `c` | bool | Whether zstd compression was applied |

JSON keys are kept short to minimise index size. The entire index is also zstd-compressed, which handles path-prefix deduplication very effectively.

## Pre-Compressed Format Detection

Files in already-compressed formats automatically skip zstd compression. Detection uses two methods (either matching is sufficient):

**By extension:** `.zip`, `.gz`, `.bz2`, `.xz`, `.zst`, `.rar`, `.7z`, `.lz4`, `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.avif`, `.mp4`, `.mp3`, `.mkv`, `.mov`, `.ogg`, `.flac`, `.woff`, `.woff2`, and more.

**By magic bytes:** PNG (`\x89PNG`), JPEG (`\xFF\xD8\xFF`), GIF (`GIF8`), ZIP (`PK`), gzip (`\x1F\x8B`), zstd (`\x28\xB5\x2F\xFD`), MP4 (`ftyp` at offset 4), MP3 (ID3/frame sync), RIFF (WebP/AVI), Matroska/WebM, and more.

Override auto-detection with `AddFileOptions.ForceCompress`.

## Package Structure

```
packager/
├── doc.go              # Package documentation
├── entry.go            # FileEntry, MasterIndex types
├── detect.go           # Pre-compressed format detection
├── writer.go           # ArchiveWriter (sequential WORM builder)
├── reader.go           # ArchiveReader (O(1) random-access)
├── pipeline/
│   └── pipeline.go     # zstd compression + ChaCha20-Poly1305 encryption
└── storage/
    ├── storage.go       # ObjectReader interface
    ├── local.go         # Local file implementation
    ├── mem.go           # In-memory implementation (testing)
    ├── s3.go            # AWS S3 implementation
    └── gcs.go           # Google Cloud Storage implementation
```

## Design Decisions

- **ChaCha20-Poly1305** over AES-GCM: faster in software (no AES-NI required), constant-time, 256-bit key.
- **zstd** over gzip/lz4: best decompression speed-to-ratio trade-off; multi-GB/s decode.
- **Indexed-Tail** over Central-Directory (zip) or Header-based (tar): enables 2-request retrieval without scanning; index is encrypted hiding directory structure.
- **Per-file nonces**: each file gets a unique random 12-byte nonce, preventing key-reuse vulnerabilities even with a single master key.
- **Master index always encrypted+compressed**: even if individual files opt out of encryption, the directory structure is always protected.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
