package riscos

import (
	"testing"
	"time"
)

func TestFileType(t *testing.T) {
	tests := []struct {
		load uint32
		want int
	}{
		{0xffffeb4b, 0xfeb},
		{0xfffff800, 0xff8},
		{0xfffffdc0, 0xffd},
		{0xfffddc5c, 0xddc},
		{0xfff3fb00, 0x3fb},
		{0x00008000, -1},
		{0x00000000, -1},
	}
	for _, tt := range tests {
		got := FileType(tt.load)
		if got != tt.want {
			t.Errorf("FileType(0x%08x) = 0x%x, want 0x%x", tt.load, got, tt.want)
		}
	}
}

func TestAppendFileType(t *testing.T) {
	tests := []struct {
		name string
		load uint32
		want string
	}{
		{"!Run", 0xffffeb00, "!Run,feb"},
		{"Data", 0xfffffdc0, "Data,ffd"},
		{"plain", 0x00008000, "plain"},
	}
	for _, tt := range tests {
		got := AppendFileType(tt.name, tt.load)
		if got != tt.want {
			t.Errorf("AppendFileType(%q, 0x%08x) = %q, want %q", tt.name, tt.load, got, tt.want)
		}
	}
}

func TestTranslateFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"!Run", "!Run"},
		{"src.c", "src/c"},
		{"file/txt", "file.txt"},
		{"dir.sub/ext", "dir/sub.ext"},
	}
	for _, tt := range tests {
		got := TranslateFilename(tt.in)
		if got != tt.want {
			t.Errorf("TranslateFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsDirectory(t *testing.T) {
	tests := []struct {
		load uint32
		want bool
	}{
		{0xfffddc5c, true},
		{0xfffddc00, true},
		{0xffffeb00, false},
		{0x00000000, false},
	}
	for _, tt := range tests {
		got := IsDirectory(tt.load)
		if got != tt.want {
			t.Errorf("IsDirectory(0x%08x) = %v, want %v", tt.load, got, tt.want)
		}
	}
}

func TestFileTime(t *testing.T) {
	// 2 Feb 1970 00:00:00 UTC: centiseconds since 1 Jan 1900 =
	// (70*365.25*86400 + 32*86400) * 100 approximately.
	// load low byte = 0x33, exec = 0x7DCE6E00 encodes this date.
	ft := FileTime(0xffffff33, 0x7f142a00).UTC()
	if ft.Year() != 1970 || ft.Month() != time.February || ft.Day() != 2 {
		t.Errorf("FileTime(0xffffff33, 0x7f142a00) = %v, want 1970-02-02", ft)
	}

	// Verify a known timestamp from the arcfs.arc test archive:
	// !ArcFS !Help file dated 2003-03-17
	ft2 := FileTime(0xffffeb4b, 0xd419aee4).UTC()
	if ft2.Year() != 2003 || ft2.Month() != time.March {
		t.Errorf("FileTime(0xffffeb4b, 0xd419aee4) = %v, want 2003-03", ft2)
	}
}
