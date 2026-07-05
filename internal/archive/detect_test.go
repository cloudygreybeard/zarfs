package archive

import (
	"bytes"
	"testing"
)

func TestDetectReader(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   Format
	}{
		{"Spark", []byte{0x1a, 0x82, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, FormatSpark},
		{"Spark high bit", []byte{0x1a, 0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, FormatSpark},
		{"ARC (not Spark)", []byte{0x1a, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, FormatUnknown},
		{"ArcFS", []byte("Archive\x00"), FormatArcFS},
		{"PackDir", []byte("PACK\x00\x00\x00\x00"), FormatPackDir},
		{"Squash", []byte("SQSH\x00\x00\x00\x00"), FormatSquash},
		{"CFS", []byte{0x00, 0x00, 0x00, 0x00, 0x03, 0x03, 0x00, 0x00}, FormatCFS},
		{"Unknown", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, FormatUnknown},
		{"Too short", []byte{0x1a}, FormatUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.header)
			got, err := DetectReader(r)
			if err != nil {
				t.Fatalf("DetectReader: %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectReader = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatString(t *testing.T) {
	tests := []struct {
		f    Format
		want string
	}{
		{FormatSpark, "Spark"},
		{FormatArcFS, "ArcFS"},
		{FormatPackDir, "PackDir"},
		{FormatSquash, "Squash"},
		{FormatCFS, "CFS"},
		{FormatUnknown, "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.f.String(); got != tt.want {
			t.Errorf("Format(%d).String() = %q, want %q", tt.f, got, tt.want)
		}
	}
}
