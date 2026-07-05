# Archive Format Reference

This document describes the archive formats supported by zarfs.

---

## Tar

zarfs supports standard POSIX tar archives (read-write) and
gzip-compressed tar archives (read-write).

### Identification

- **Tar:** bytes `ustar\0` or `ustar ` at offset 257
- **Gzip (tar.gz):** bytes `0x1F 0x8B` at offset 0, containing a tar stream

### Write behaviour

New files are written uncompressed within the tar stream. On flush,
zarfs rewrites the entire tar from its in-memory state. For tar.gz,
the archive is decompressed to a temporary file on open and
recompressed on flush.

### References

- [POSIX.1-2001 tar format](https://pubs.opengroup.org/onlinepubs/9699919799/utilities/pax.html)
- [Go archive/tar](https://pkg.go.dev/archive/tar)
- [Go compress/gzip](https://pkg.go.dev/compress/gzip)

---

## RISC OS Formats

The following sections describe the RISC OS-specific archive formats
supported by zarfs and the metadata conventions they share.

## RISC OS File Metadata

RISC OS does not use filename extensions. Instead, each file carries a
12-bit file type and a 40-bit timestamp encoded in two 32-bit fields
called the load address and execution address.

### File Type and Timestamp Encoding

When the top 12 bits of the load address are `0xFFF`, the remaining bits
encode the file type and timestamp:

    Load address:  0xFFFtttdd
    Exec address:  0xdddddddd

Where `ttt` is the 12-bit file type (displayed as three hex digits) and
`dddddddddd` is a 40-bit unsigned integer counting centiseconds (0.01
seconds) since 00:00:00 UTC on 1 January 1900.

When the top 12 bits are not `0xFFF`, the fields are literal memory
addresses (load and execution) with no embedded type or timestamp.

**References:**
- [RISC OS PRMs: FileSwitch](https://www.riscos.com/support/developers/prm/fileswitch.html)
- [RISC OS Open: FileSwitch Key Features](https://www.riscosopen.org/wiki/documentation/show/FileSwitch%20Key%20Features)

### Common File Types

| Hex   | Name         | Description                          |
|-------|--------------|--------------------------------------|
| `3FB` | ArcFSArc     | ArcFS archive                        |
| `68E` | PackDir      | PackDir archive                      |
| `D96` | CFSlzw       | CFS compressed file                  |
| `DDC` | Archive      | Spark/SparkFS archive                |
| `FCA` | Squash       | Squash compressed file               |
| `FEB` | Obey         | Command script                       |
| `FF8` | Absolute     | Relocatable module (executable)      |
| `FF9` | Sprite       | Sprite graphics file                 |
| `FFA` | Module       | Relocatable module                   |
| `FFB` | BASIC        | BASIC program                        |
| `FFD` | Data         | Generic data file                    |
| `FFF` | Text         | Plain text file                      |

**Reference:** [RISC OS Open: File Types](https://www.riscosopen.org/wiki/documentation/show/File%20Types)

### File Attributes

File attributes are stored as a 32-bit word. The low byte contains
standardised permission bits:

| Bit | Meaning when set                      |
|-----|---------------------------------------|
| 0   | Owner read access                     |
| 1   | Owner write access                    |
| 2   | Reserved (owner execute on BBC ADFS)  |
| 3   | Locked against deletion by owner      |
| 4   | Public read access                    |
| 5   | Public write access                   |
| 6   | Reserved                              |
| 7   | Reserved                              |

**Reference:** [RISC OS PRMs: FileSwitch - File attributes](https://www.riscos.com/support/developers/prm/fileswitch.html)

### Filename Translation

RISC OS uses `.` as the directory separator and `/` as an extension
separator (the reverse of Unix). When extracting files, Spark-family
tools swap these characters: RISC OS `/` becomes local `.` and RISC OS
`.` becomes local `/`.

zarfs appends the file type as a comma-separated suffix (e.g.
`!Run,feb`) following the convention established by nspark and RISC OS
on Unix.

---

## Spark (Filetype DDC)

Spark is a RISC OS variant of the SEA ARC archive format, extended with
12 extra bytes per entry to carry RISC OS file attributes.

### Identification

- Byte 0: `0x1A` (archive marker)
- Byte 1: compression method with bit 7 set (values `0x80`--`0xFF`)

If byte 1 has bit 7 clear, the file is a PC-format ARC archive rather
than Spark.

### Entry Structure

An archive consists of sequential `(marker, header, data)` tuples. The
archive terminates with a marker byte (`0x1A`) followed by a zero byte.

| Field           | Size    | Notes                                     |
|-----------------|---------|-------------------------------------------|
| Marker          | 1 byte  | Always `0x1A`                             |
| Compression     | 1 byte  | Method + `0x80` (ARCHPACK flag)           |
| Filename        | 13 bytes| NUL-terminated, max 12 printable chars    |
| Compressed size | 4 bytes | Little-endian uint32                      |
| Date            | 2 bytes | MS-DOS format date                        |
| Time            | 2 bytes | MS-DOS format time                        |
| CRC             | 2 bytes | CRC-16                                    |
| Original size   | 4 bytes | Present only if method > 1                |
| Load address    | 4 bytes | Present only if ARCHPACK (bit 7) set      |
| Exec address    | 4 bytes | Present only if ARCHPACK (bit 7) set      |
| Attributes      | 4 bytes | Present only if ARCHPACK (bit 7) set      |

After the header, `compressed_size` bytes of (possibly compressed) data
follow.

### Compression Methods

| ID (7-bit) | Name       | Algorithm                              |
|------------|------------|----------------------------------------|
| 0          | End-of-dir | Directory end marker                   |
| 1          | Stored     | No compression (old style)             |
| 2          | Stored     | No compression (new style)             |
| 3          | Packed     | RLE (run marker `0x90`)                |
| 4          | Squeezed   | Huffman + RLE                          |
| 8          | Crunched   | 12-bit LZW + RLE                      |
| 9          | Squashed   | 13-bit LZW (PKARC style)              |
| 127        | Compressed | LZW, configurable max bits (12--16)    |

### Directories

Directories are represented as nested archives. A directory entry has
`load_address & 0xFFFFFF00 == 0xFFDDC00` and is followed by its
children in the archive stream, terminated by an end-of-directory marker
(method 0).

**References:**
- [spark(5) man page](https://manpages.ubuntu.com/manpages/noble/man5/spark.5.html)
- [Archive Team: Spark](http://fileformats.archiveteam.org/wiki/Spark)
- [David Pilling: SparkFS manual (PDF)](https://www.davidpilling.com/software/SparkFS.pdf)
- [ARC format (Wikipedia)](https://en.wikipedia.org/wiki/ARC_(file_format))

---

## ArcFS (Filetype 3FB)

ArcFS is a random-access archive format for RISC OS with a flat
directory table at the front and file data at a known offset.

### Identification

Bytes 0--7: `"Archive\0"` (ASCII string followed by NUL).

### File Header

| Offset | Size    | Field                                    |
|--------|---------|------------------------------------------|
| 0      | 8 bytes | `"Archive\0"` signature                  |
| 8      | 4 bytes | Header length (directory size in bytes)   |
| 12     | 4 bytes | Data start offset                        |
| 16     | 4 bytes | Format version (reject if > 40)          |
| 20     | 4 bytes | Read/write version                       |
| 24     | 4 bytes | Archive format                           |
| 28     | 68 bytes| Reserved (17 x uint32)                   |

### Entry Records

`num_entries = header_length / 36`. Each entry is 36 bytes:

| Offset | Size    | Field                                    |
|--------|---------|------------------------------------------|
| 0      | 1 byte  | Compression type                         |
| 1      | 11 bytes| Filename (NUL-terminated)                |
| 12     | 4 bytes | Original size (`0xFFFFFFFF` for dirs)    |
| 16     | 4 bytes | Load address                             |
| 20     | 4 bytes | Exec address                             |
| 24     | 4 bytes | Packed: bits 16--31 CRC, 8--15 maxbits, 0--7 attributes |
| 28     | 4 bytes | Compressed size (`0xFFFFFFFF` for dirs)  |
| 32     | 4 bytes | Info word: bit 31 = directory flag; bits 0--30 = data offset from data start |

### Compression Types

| ID     | Name       | Algorithm                              |
|--------|------------|----------------------------------------|
| `0x00` | End        | End-of-directory marker                |
| `0x01` | Deleted    | Deleted entry (skipped)                |
| `0x82` | Stored     | No compression                         |
| `0x83` | Packed     | RLE                                    |
| `0x88` | Crunched   | 12-bit LZW + RLE                      |
| `0xFF` | Compressed | LZW with maxbits from packed word      |

**References:**
- [arcfs(5) man page](https://manpages.ubuntu.com/manpages/noble/man5/arcfs.5.html)
- [Archive Team: ArcFS](http://fileformats.archiveteam.org/wiki/ArcFS)
- [ARM Club: ArcFS](https://armclub.org.uk/products/arcfs/)

---

## PackDir (Filetype 68E)

PackDir is a tree-structured archive format using Zoo-variant LZW
compression. Written by John Kortink.

### Identification

Bytes 0--4: `"PACK\0"` (ASCII string followed by NUL).

### Header

| Offset | Size    | Field                                    |
|--------|---------|------------------------------------------|
| 0      | 5 bytes | `"PACK\0"` signature                    |
| 5      | 4 bytes | LZW extra bits (actual max = value + 12) |

### Entry Records

Entries are sequential in depth-first tree order:

| Field       | Type              | Notes                            |
|-------------|-------------------|----------------------------------|
| Name        | NUL-terminated    | ASCII string                     |
| Load        | uint32            | RISC OS load address             |
| Exec        | uint32            | RISC OS exec address             |
| N           | uint32            | File: original size; Dir: child count |
| Attributes  | uint32            | RISC OS permissions              |
| Entry type  | uint32            | 1 = directory, 0 = file (omitted for root entry) |

File entries additionally contain:

| Field            | Type   | Notes                             |
|------------------|--------|-----------------------------------|
| Compressed size  | uint32 | `0xFFFFFFFF` means stored (no compression) |
| Compression type | uint32 | Present only when compressed; LZW |
| Data             | N bytes| Compressed or raw file data       |

The LZW variant is the same as Zoo: CLEAR code first, EOF code 257,
byte-aligned bit reads.

**References:**
- [Archive Team: PackDir](http://fileformats.archiveteam.org/wiki/PackDir)
- [riscosarc doc/packdir.txt](https://github.com/mjwoodcock/riscosarc/blob/master/doc/packdir.txt)
- [XAD PackDir client](https://www.kyzer.me.uk/pack/xad/#PackDir)

---

## Squash (Filetype FCA)

Squash is a single-file compression format built into RISC OS. The
Squash SWI module uses 12-bit LZW (Unix compress compatible).

### Identification

Bytes 0--3: `"SQSH"`.

### Header (20 bytes)

| Offset | Size    | Field                                    |
|--------|---------|------------------------------------------|
| 0      | 4 bytes | `"SQSH"` signature                      |
| 4      | 4 bytes | Original uncompressed size               |
| 8      | 4 bytes | Load address                             |
| 12     | 4 bytes | Exec address                             |
| 16     | 4 bytes | Reserved (0)                             |

### Compressed Data

The remaining data (offset 20 to EOF) is a standard Unix compress
stream beginning with the `0x1F 0x9D` magic bytes and a maxbits/flags
byte. It can be decompressed with `gunzip` or `uncompress`.

The filename is derived from the archive filename by stripping the
`,fca` suffix.

**References:**
- [RISC OS Open: Squash file format](https://www.riscosopen.org/wiki/documentation/show/File%20formats:%20Squash%20file)
- [RISC OS PRMs: Squash module](https://www.riscos.com/support/developers/prm/squash.html)
- [Archive Team: Squash](http://fileformats.archiveteam.org/wiki/Squash_(RISC_OS))

---

## CFS (Filetype D96)

CFS (Compacted File System) is a single-file compression format from
Computer Concepts' Compression utility. It uses a block-based 12-bit
LZW algorithm (David Pilling's FileShrinker algorithm).

### Identification

Bytes 4--7: `0x03 0x03 0x00 0x00` (uint32 LE value `0x303` at offset 4).

### Header

| Offset | Size    | Field                                    |
|--------|---------|------------------------------------------|
| 0      | 4 bytes | Unknown (read and skipped)               |
| 4      | 4 bytes | Magic: `0x00000303`                      |
| 8      | 4 bytes | Original uncompressed size               |
| 12     | 4 bytes | Reserved                                 |
| 16     | 4 bytes | Load address                             |
| 20     | 4 bytes | Exec address                             |

### Block Stream

The compressed data is a sequence of blocks, each with a 4-byte header:

| Field      | Size    | Notes                                  |
|------------|---------|----------------------------------------|
| Block type | 1 byte  | See table below                        |
| Length     | 3 bytes | Little-endian; interpretation varies   |

| Type | Name       | Content                                 |
|------|------------|-----------------------------------------|
| 0x00 | End        | End of stream                           |
| 0x01 | Compressed | LZW data; length = code limit + `0xFF`  |
| 0x02 | Raw        | Uncompressed data; length = byte count  |
| 0x03 | Header     | Block header (metadata)                 |
| 0x04 | Zero       | Zero-filled data block                  |

The LZW uses 12-bit fixed codes, starting at 9 bits.

**References:**
- [Archive Team: CFS](http://fileformats.archiveteam.org/wiki/CFS_(Computer_Concepts_Compression))
- [Computer Concepts: Compression](http://www.cconcepts.co.uk/products/compr.htm)
- [Computer Concepts: CFSReader](http://www.cconcepts.co.uk/support/cfsread.htm)

---

## Compression Algorithms

### RLE (Pack)

Run-length encoding using `0x90` as the run marker:
- `0x90 0x00` encodes a literal `0x90` byte
- `0x90 N` repeats the previous byte `N-1` times

Used by Spark (method 3), ArcFS (`0x83`), and as a post-filter after
LZW in Crunch and PackSqueeze methods.

### LZW (ncompress family)

Variable-width LZW based on the Unix `compress` utility (ncompress).
Several variants exist:

| Variant      | Header        | Max bits | Block mode | Notes           |
|--------------|---------------|----------|------------|-----------------|
| Unix compress| `1F 9D xx`    | From hdr | From hdr   | Standard        |
| Compress     | maxbits byte  | 12--16   | Yes        | Spark method 127|
| Crunch       | maxbits byte  | 12       | Yes        | + RLE post-filter|
| Squash       | None          | 13       | Yes        | PKARC style     |

All variants use CLEAR code 256 and start dictionary entries at 257.
Codes are read in blocks of `n_bits` bytes; when the code width changes
the remainder of the current block is discarded.

**References:**
- [ncompress source](https://github.com/vapier/ncompress)
- [LZC format notes](https://ciderpress2.com/formatdoc/LZC-notes.html)
- [compress(1) man page](https://manpages.debian.org/bullseye/ncompress/compress.1.en.html)

### LZW (Zoo/PackDir variant)

Zoo-style LZW used by PackDir:
- First code after CLEAR is a data byte (not a regular code)
- EOF code = 257
- Byte-aligned bit reading (no block alignment)
- Max bits configurable (header value + 12)

### Huffman (PackSqueeze)

Spark method 4 applies Huffman coding followed by RLE. The Huffman tree
is stored as an array of `(left, right)` uint16 node indices at the
start of the compressed data.

---

## Reference Implementations

| Name       | Language | Repository                                              |
|------------|----------|---------------------------------------------------------|
| riscosarc  | Java     | [mjwoodcock/riscosarc](https://github.com/mjwoodcock/riscosarc) |
| nspark     | C        | [mjwoodcock/nspark](https://github.com/mjwoodcock/nspark) |
| SparkFS    | C (ARM)  | [RISC OS Open GitLab](https://gitlab.riscosopen.org/RiscOS/Sources/Apps/SparkFS) |
| Deark      | C        | [jsummers/deark](https://github.com/jsummers/deark)     |
