package compress

import (
	"bytes"
	"io"
	"testing"
)

func TestPackReader(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []byte
	}{
		{
			"no runs",
			[]byte("hello"),
			[]byte("hello"),
		},
		{
			"literal 0x90",
			[]byte{0x90, 0x00},
			[]byte{0x90},
		},
		{
			"run of 5",
			[]byte{'A', 0x90, 0x05},
			[]byte{'A', 'A', 'A', 'A', 'A'},
		},
		{
			"mixed",
			[]byte{'X', 0x90, 0x03, 'Y'},
			[]byte{'X', 'X', 'X', 'Y'},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewPackReader(bytes.NewReader(tt.input))
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
