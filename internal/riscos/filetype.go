// Package riscos provides utilities for RISC OS file metadata.
package riscos

import "fmt"

// FileType returns the 12-bit RISC OS filetype from a load address.
// Returns -1 if the load address does not encode a filetype.
func FileType(load uint32) int {
	if load&0xfff00000 != 0xfff00000 {
		return -1
	}
	return int((load >> 8) & 0xfff)
}

// AppendFileType appends a ,xxx filetype suffix to name if the load
// address encodes a RISC OS filetype.
func AppendFileType(name string, load uint32) string {
	ft := FileType(load)
	if ft < 0 {
		return name
	}
	return fmt.Sprintf("%s,%03x", name, ft)
}

// TranslateFilename converts a RISC OS filename to a local path by
// swapping '/' and '.' characters.
func TranslateFilename(roname string) string {
	out := make([]byte, len(roname))
	for i := range roname {
		switch roname[i] {
		case '/':
			out[i] = '.'
		case '.':
			out[i] = '/'
		default:
			out[i] = roname[i]
		}
	}
	return string(out)
}

// IsDirectory returns true if the load address indicates a Spark
// directory entry (filetype 0xDDC with exec 0x00).
func IsDirectory(load uint32) bool {
	return load&0xffffff00 == 0xfffddc00
}
