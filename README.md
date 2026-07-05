# zarfs

A *zarf* (from the Arabic for "envelope", "wrapper", or "receptacle") is
a tool for holding a hot coffee cup, letting you handle its contents
comfortably.

`zarfs` is a tool which provides a virtual filesystem for an archive file,
letting you handle its contents comfortably.

zarfs supports tar, gzip-compressed tar, and some common RISC OS archive
formats, too, transparently decompressing files on access. Tar, tar.gz, and
ArcFS archives can be mounted read-write; all other formats are
read-only.

zarfs supports two mount transports: kernel FUSE and an embedded
NFSv3 server. When FUSE is available (Linux, or macOS with macFUSE),
it is used by default. Otherwise, zarfs falls back to the embedded
NFS server, which relies on the operating system's built-in NFS client.
Either transport can be selected explicitly with `--transport`.

## Supported Formats

| Format  | Extension      | Read | Write | Description                          |
|---------|----------------|------|-------|--------------------------------------|
| Tar     | .tar           | Yes  | Yes   | POSIX tape archive                   |
| TarGz   | .tar.gz, .tgz  | Yes  | Yes   | Gzip-compressed tar                  |
| Spark   | ,ddc           | Yes  | No    | RISC OS variant of SEA ARC archives  |
| ArcFS   | ,3fb           | Yes  | Yes   | RISC OS random-access archive        |
| PackDir | ,68e           | Yes  | No    | RISC OS tree-structured archive      |
| Squash  | ,fca           | Yes  | No    | RISC OS single-file LZW compression  |
| CFS     | ,d96           | Yes  | No    | Computer Concepts Compacted FS       |

Formats are auto-detected from file header magic bytes. When
auto-detection is not possible (e.g. for an empty archive created with
`zarfs create`), use `--format` to specify the format explicitly.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install cloudygreybeard/tap/zarfs
```

### From source

```bash
git clone https://github.com/cloudygreybeard/zarfs
cd zarfs
make install
```

## Usage

### Mounting an archive

```bash
zarfs mount archive.tar.gz /mnt/archive
```

The archive contents become available as a filesystem at the mount
point. Files are decompressed transparently on read.

By default, zarfs daemonises and returns once the filesystem is ready.
Use `-f` to keep it in the foreground:

```bash
zarfs mount -f archive.tar /mnt/archive
```

To unmount:

```bash
umount /mnt/archive
```

### Listing archive contents

```bash
zarfs list archive.tar.gz
```

Verbose mode shows file types, sizes, and timestamps:

```bash
zarfs list -v archive.tar
```

### Password-protected archives

Some Spark and ArcFS archives are garbled with a password. Set the
`ZARFS_PASSWORD` environment variable or use `-p`:

```bash
ZARFS_PASSWORD=secret zarfs mount archive.spark /mnt/archive
zarfs list -p - archive.spark   # prompts on stdin
```

### Transport selection

zarfs auto-detects an available mount transport. When FUSE is available
(Linux, or macOS with macFUSE), it is used by default. Otherwise,
zarfs falls back to an embedded NFSv3 server that relies on the
operating system's built-in NFS client.

Override with `--transport`:

```bash
zarfs mount --transport=fuse archive.tar /mnt/archive
zarfs mount --transport=nfs  archive.tar /mnt/archive
```

### Creating a new archive

```bash
zarfs create tar myarchive.tar
zarfs create targz myarchive.tar.gz
zarfs create arcfs myarchive.arcfs
```

The new archive is empty and ready to be mounted read-write:

```bash
zarfs mount --format tar myarchive.tar /mnt/work
```

### Specifying format explicitly

When zarfs cannot auto-detect the format from file contents (for
example, after `zarfs create`), use `--format`:

```bash
zarfs mount --format tar archive.tar /mnt/archive
zarfs list --format targz archive.tar.gz
```

Valid format names: `tar`, `targz` (or `tar.gz`, `tgz`), `arcfs`,
`spark`, `packdir`, `squash`, `cfs`.

### Read-only mounting

Writable formats (Tar, TarGz, ArcFS) are mounted read-write by
default. Force read-only with `--read-only`:

```bash
zarfs mount --read-only archive.tar /mnt/archive
```

## Architecture

zarfs separates archive parsing from mount transport:

```
cmd/                CLI (Cobra)
internal/
  riscos/           RISC OS metadata (file types, timestamps)
  compress/         Decompression algorithms (RLE, LZW, Huffman)
  archive/          Format parsers
    tar/            Tar archives
    targz/          Gzip-compressed tar
    spark/          RISC OS Spark
    arcfs/          RISC OS ArcFS
    packdir/        RISC OS PackDir
    squash/         RISC OS Squash
    cfs/            RISC OS CFS
  arcfs/            Transport-agnostic virtual filesystem
  fusemount/        FUSE adapter (hanwen/go-fuse)
  nfsmount/         NFS adapter (willscott/go-nfs)
```

The core virtual filesystem (`internal/arcfs`) reads an archive into an
inode tree that can be served by either transport adapter. Both adapters
are pure Go with no CGO dependency (`CGO_ENABLED=0`).

## Prerequisites

The NFS transport uses the operating system's built-in NFS client
(available on Linux and macOS). The FUSE transport requires:

- **Linux:** FUSE support, typically pre-installed or available as the
  `fuse3` package
- **macOS:** [macFUSE](https://osxfuse.github.io/)

## Documentation

See [docs/formats.md](docs/formats.md) for detailed binary format
specifications, compression algorithm descriptions, and links to
reference implementations and standards.

## Development

```bash
make build       # Build the binary
make build-all   # Cross-compile for all platforms
make test        # Run tests with race detector
make lint        # Run golangci-lint
make snapshot    # Build snapshot release via GoReleaser
make clean       # Remove build artifacts
make help        # Show all targets
```

## Related Projects

- [google/fuse-archive](https://github.com/google/fuse-archive): C++
  read-only FUSE for archives via libarchive (Linux only)
- [archivemount-ng](https://git.sr.ht/~nabijaczleweli/archivemount-ng):
  C++ read-write archive FUSE via libarchive
- [riscosarc](https://github.com/mjwoodcock/riscosarc): Java
  de-archiver for RISC OS archive formats
- [nspark](https://github.com/mjwoodcock/nspark): C de-archiver for
  Spark and ArcFS archives
- [SparkFS](https://gitlab.riscosopen.org/RiscOS/Sources/Apps/SparkFS):
  RISC OS archive filing system (open source, maintained by ROOL)
- [ArcFS](https://armclub.org.uk/products/arcfs/): ARM Club's archive
  filing system for RISC OS

## Credits

See [CREDITS.md](CREDITS.md) for acknowledgements of the RISC OS
community members whose work on archive formats, reference
implementations, and documentation made this project possible.

## License

Apache 2.0. See [LICENSE](LICENSE).
