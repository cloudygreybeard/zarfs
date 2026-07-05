package archive

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

const cfsMagic = 0x303

// Format identifies an archive format.
type Format int

const (
	FormatUnknown Format = iota
	FormatSpark
	FormatArcFS
	FormatPackDir
	FormatSquash
	FormatCFS
	FormatTar
	FormatTarGz
)

// String returns a human-readable format name.
func (f Format) String() string {
	switch f {
	case FormatSpark:
		return "Spark"
	case FormatArcFS:
		return "ArcFS"
	case FormatPackDir:
		return "PackDir"
	case FormatSquash:
		return "Squash"
	case FormatCFS:
		return "CFS"
	case FormatTar:
		return "Tar"
	case FormatTarGz:
		return "TarGz"
	default:
		return "Unknown"
	}
}

// Detect examines the first bytes of the file at path and returns the
// archive format. Returns FormatUnknown if the format is not
// recognised.
func Detect(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown, err
	}
	defer func() { _ = f.Close() }()

	return DetectReader(f)
}

// ParseFormat converts a user-supplied format name to a Format
// constant. Returns FormatUnknown and an error for unrecognised names.
func ParseFormat(name string) (Format, error) {
	switch strings.ToLower(name) {
	case "tar":
		return FormatTar, nil
	case "targz", "tar.gz", "tgz":
		return FormatTarGz, nil
	case "arcfs":
		return FormatArcFS, nil
	case "spark":
		return FormatSpark, nil
	case "packdir":
		return FormatPackDir, nil
	case "squash":
		return FormatSquash, nil
	case "cfs":
		return FormatCFS, nil
	default:
		return FormatUnknown, fmt.Errorf("unknown format %q", name)
	}
}

// DetectReader examines the first bytes from r (which must also
// implement io.Seeker) and returns the archive format.
func DetectReader(r io.ReadSeeker) (Format, error) {
	var hdr [8]byte
	n, err := r.Read(hdr[:])
	if err != nil && n == 0 {
		return FormatUnknown, fmt.Errorf("reading header: %w", err)
	}

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return FormatUnknown, fmt.Errorf("seeking: %w", err)
	}

	if n >= 2 && hdr[0] == 0x1a && hdr[1]&0x80 != 0 {
		return FormatSpark, nil
	}

	if n >= 8 && string(hdr[:8]) == "Archive\x00" {
		return FormatArcFS, nil
	}

	if n >= 5 && string(hdr[:5]) == "PACK\x00" {
		return FormatPackDir, nil
	}

	if n >= 4 && string(hdr[:4]) == "SQSH" {
		return FormatSquash, nil
	}

	if n >= 8 {
		magic := binary.LittleEndian.Uint32(hdr[4:8])
		if magic == cfsMagic {
			return FormatCFS, nil
		}
	}

	if n >= 2 && hdr[0] == 0x1f && hdr[1] == 0x8b {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return FormatUnknown, fmt.Errorf("seeking: %w", err)
		}
		gr, err := gzip.NewReader(r)
		if err == nil {
			var inner [263]byte
			n2, _ := io.ReadFull(gr, inner[:])
			_ = gr.Close()
			if n2 >= 263 && (string(inner[257:263]) == "ustar\x00" || string(inner[257:263]) == "ustar ") {
				if _, err := r.Seek(0, io.SeekStart); err != nil {
					return FormatUnknown, fmt.Errorf("seeking: %w", err)
				}
				return FormatTarGz, nil
			}
		}
	}

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return FormatUnknown, fmt.Errorf("seeking: %w", err)
	}
	var tarBuf [263]byte
	n2, _ := io.ReadFull(r, tarBuf[:])
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return FormatUnknown, fmt.Errorf("seeking: %w", err)
	}
	if n2 >= 263 && (string(tarBuf[257:263]) == "ustar\x00" || string(tarBuf[257:263]) == "ustar ") {
		return FormatTar, nil
	}

	return FormatUnknown, nil
}
