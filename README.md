# packager

A high-performance Go library that packs multiple files into a single encrypted, compressed archive optimised for cloud object storage (S3, GCS, local disk). It solves the "small file problem" by enabling **O(1) random-access** retrieval of individual files over the network without downloading or scanning the entire archive.

## Features

- **Indexed-Tail format** — sequential data blocks + encrypted master index + 8-byte footer. Retrieves any file in 2–3 byte-range requests.
- **Per-file encryption** — ChaCha20-Poly1305 (AEAD) with unique random nonces. Opt out per file or keep everything encrypted.
- **Ultra-fast compression** — zstd via [klauspost/compress](https://github.com/klauspost/compress). Multi-GB/s decompression, automatic skip for already-compressed formats.
- **Smart detection** — PNG, JPEG, GIF, ZIP, MP4, and 20+ other formats are auto-detected by extension and magic bytes; compression is skipped automatically.
- **Rich metadata** — each file entry stores Unix permissions, owner UID, encryption flag, and compression flag.
- **Memory-efficient index** — `PathIndex` deduplicates directory prefixes, storing each unique directory once. 100K files in 500 dirs uses ~1.2 MB instead of ~6.2 MB of path strings.
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
store := storage.NewS3Reader(client, "my-bucket", "archives/repo.pack",
    storage.WithS3Context(ctx),
)

ar, err := packager.OpenArchive(store, p,
    packager.WithIndexSize(cachedSize), // skip footer fetch if cached
)
```

### Using with GCS

```go
store := storage.NewGCSReader(bucket, "archives/repo.pack",
    storage.WithGCSContext(ctx),
)

ar, err := packager.OpenArchive(store, p)
```

## Cloud Storage Setup

Both AWS S3 and GCS backends work **inside and outside** of cloud environments.
When running inside the cloud (EC2, ECS, EKS, GCE, Cloud Run, GKE, …) credentials are picked up automatically from the instance metadata / workload identity.
When running externally (local machine, CI, on-prem) you supply credentials explicitly via config flags, environment variables, or credential files.

### AWS S3

#### Configuration

`storage.S3Config` drives client creation:

| Field | Type | Required | Description |
|---|---|---|---|
| `Region` | `string` | **yes** | AWS region, e.g. `"us-east-1"` |
| `Endpoint` | `string` | no | Custom endpoint URL (MinIO, Ceph, LocalStack, …) |
| `AccessKeyID` | `string` | no | Static IAM access key |
| `SecretAccessKey` | `string` | no | Static IAM secret key |
| `SessionToken` | `string` | no | STS session token for temporary credentials |
| `UsePathStyle` | `bool` | no | Path-style addressing (`http://s3/bucket/key` instead of `http://bucket.s3/key`). Required for most S3-compatible services. |

#### Credential resolution order

1. **Static credentials** — if `AccessKeyID` + `SecretAccessKey` are set, they are used directly.
2. **Default AWS credential chain** (automatic) — environment variables → `~/.aws/credentials` shared file → IAM role (EC2 instance profile / ECS task role / EKS IRSA).

#### Running inside AWS (EC2, ECS, EKS, Lambda)

No explicit credentials needed — the SDK picks up the attached IAM role automatically:

```go
store, err := storage.NewS3ReaderFromConfig(ctx, storage.S3Config{
    Region: "us-east-1",
}, "my-bucket", "archives/repo.pack")
```

#### Running outside AWS (local dev, CI, on-prem)

**Option A — environment variables** (recommended for CI):

```bash
export AWS_ACCESS_KEY_ID=AKIA…
export AWS_SECRET_ACCESS_KEY=wJalr…
export AWS_REGION=us-east-1
```

```go
store, err := storage.NewS3ReaderFromConfig(ctx, storage.S3Config{
    Region: "us-east-1",
}, "my-bucket", "archives/repo.pack")
```

**Option B — explicit credentials in config** (useful for programmatic setup):

```go
store, err := storage.NewS3ReaderFromConfig(ctx, storage.S3Config{
    Region:         "us-east-1",
    AccessKeyID:    os.Getenv("AWS_ACCESS_KEY_ID"),
    SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
}, "my-bucket", "archives/repo.pack")
```

**Option C — AWS CLI shared credentials** (`~/.aws/credentials`):

```bash
aws configure   # sets up ~/.aws/credentials
```

```go
// SDK reads ~/.aws/credentials automatically
store, err := storage.NewS3ReaderFromConfig(ctx, storage.S3Config{
    Region: "us-east-1",
}, "my-bucket", "archives/repo.pack")
```

#### Using with S3-compatible services (MinIO, Ceph, LocalStack)

```go
store, err := storage.NewS3ReaderFromConfig(ctx, storage.S3Config{
    Region:         "us-east-1",
    Endpoint:       "http://localhost:9000",
    AccessKeyID:    "minioadmin",
    SecretAccessKey: "minioadmin",
    UsePathStyle:   true,
}, "my-bucket", "archives/repo.pack")
```

#### Advanced: bring your own client

If you need full control over the `*s3.Client` (custom HTTP transport, retry policy, etc.) you can create it yourself and pass it directly:

```go
client, err := storage.NewS3Client(ctx, storage.S3Config{
    Region: "us-east-1",
})
// … or build *s3.Client however you like …

store := storage.NewS3Reader(client, "my-bucket", "archives/repo.pack",
    storage.WithS3Context(ctx),
)
```

---

### Google Cloud Storage (GCS)

#### Configuration

`storage.GCSConfig` drives client creation:

| Field | Type | Required | Description |
|---|---|---|---|
| `CredentialsFile` | `string` | no | Path to a service account JSON key file |
| `CredentialsJSON` | `[]byte` | no | Inline service account JSON key content (takes precedence over `CredentialsFile`) |

#### Credential resolution order

1. **`CredentialsJSON`** — raw JSON key bytes provided in config.
2. **`CredentialsFile`** — path to a JSON key file on disk.
3. **Application Default Credentials (ADC)** (automatic) — `GOOGLE_APPLICATION_CREDENTIALS` env var → `gcloud auth application-default login` → GCE metadata server / GKE Workload Identity.

#### Running inside GCP (GCE, Cloud Run, GKE)

No explicit credentials needed — the metadata server or Workload Identity provides them:

```go
store, err := storage.NewGCSReaderFromConfig(ctx, storage.GCSConfig{},
    "my-bucket", "archives/repo.pack")
```

> **Note:** the GCE service account (or GKE Workload Identity SA) must have the `roles/storage.objectViewer` role on the bucket.

#### Running outside GCP (local dev, CI, on-prem)

**Option A — `gcloud` CLI** (simplest for local dev):

```bash
gcloud auth application-default login
```

```go
store, err := storage.NewGCSReaderFromConfig(ctx, storage.GCSConfig{},
    "my-bucket", "archives/repo.pack")
```

**Option B — service account key file** (recommended for CI / on-prem):

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa-key.json
```

```go
// ADC picks up the env var automatically
store, err := storage.NewGCSReaderFromConfig(ctx, storage.GCSConfig{},
    "my-bucket", "archives/repo.pack")
```

Or pass the file path explicitly:

```go
store, err := storage.NewGCSReaderFromConfig(ctx, storage.GCSConfig{
    CredentialsFile: "/path/to/sa-key.json",
}, "my-bucket", "archives/repo.pack")
```

**Option C — inline JSON key** (useful when the key is stored in a secret manager):

```go
keyJSON, _ := secretmanager.GetSecret("gcs-sa-key")

store, err := storage.NewGCSReaderFromConfig(ctx, storage.GCSConfig{
    CredentialsJSON: keyJSON,
}, "my-bucket", "archives/repo.pack")
```

#### Advanced: bring your own client

```go
client, err := storage.NewGCSClient(ctx, storage.GCSConfig{
    CredentialsFile: "/path/to/sa-key.json",
})
// … or build *storage.Client however you like …

bucket := client.Bucket("my-bucket")
store := storage.NewGCSReader(bucket, "archives/repo.pack",
    storage.WithGCSContext(ctx),
)
```

> **Tip:** when using `NewGCSReaderFromConfig`, call `store.Close()` when done — it releases the underlying GCS client. Readers created via `NewGCSReader` have a no-op `Close`.

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

JSON keys are kept short to minimise index size. The entire index is also zstd-compressed.

## Pre-Compressed Format Detection

Files in already-compressed formats automatically skip zstd compression. Detection uses two methods (either matching is sufficient):

**By extension:** `.zip`, `.gz`, `.bz2`, `.xz`, `.zst`, `.rar`, `.7z`, `.lz4`, `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.avif`, `.mp4`, `.mp3`, `.mkv`, `.mov`, `.ogg`, `.flac`, `.woff`, `.woff2`, and more.

**By magic bytes:** PNG (`\x89PNG`), JPEG (`\xFF\xD8\xFF`), GIF (`GIF8`), ZIP (`PK`), gzip (`\x1F\x8B`), zstd (`\x28\xB5\x2F\xFD`), MP4 (`ftyp` at offset 4), MP3 (ID3/frame sync), RIFF (WebP/AVI), Matroska/WebM, and more.

Override auto-detection with `AddFileOptions.ForceCompress`.

## Memory-Efficient Path Index

The in-memory index uses `PathIndex` with **directory-prefix deduplication** rather than a flat `map[string]FileEntry`. Each unique directory path is stored once in a shared table; file entries retain only their basename plus a directory ID.

```
Memory: O(D × avg_dir_len + N × avg_basename_len)  vs  O(N × avg_path_len)
```

**Example:** 100,000 files across 500 directories with 50-byte average directory prefixes:
- Flat map: ~6.2 MB of path strings
- PathIndex: ~25 KB dirs + ~1.2 MB basenames = **~1.2 MB** (5× reduction)

The on-disk serialization also uses a prefix-table format:

```json
{
  "d": ["src/pkg/handlers", "src/pkg/models", ...],
  "f": [
    {"d": 0, "n": "auth.go", "o": 0, "s": 512, "p": 420, "u": 1000, "e": true, "c": true},
    {"d": 0, "n": "user.go", "o": 512, "s": 256, "p": 420, "u": 1000, "e": true, "c": true},
    ...
  ]
}
```

This produces smaller JSON before zstd compression kicks in, as directory strings are not repeated per file.

## Package Structure

```
packager/
├── doc.go              # Package documentation
├── entry.go            # FileEntry type
├── pathindex.go        # PathIndex (dir-prefix-deduplicated file index)
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
- **Directory-prefix deduplication** over per-key compression: file paths share common prefixes (directory trees), so deduplicating directories gives 3–5× memory savings without CPU cost on lookups. Per-key zstd/gzip on short strings (avg 30–60 bytes) yields poor ratios and adds decode overhead per access.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
