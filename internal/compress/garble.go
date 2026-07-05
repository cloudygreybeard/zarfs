package compress

import "io"

// GarbleReader XORs each byte from r with cycling password bytes.
// If password is nil or empty, data passes through unchanged.
type GarbleReader struct {
	r      io.Reader
	passwd []byte
	pos    int
}

// NewGarbleReader creates a password XOR reader. Pass nil for no
// password.
func NewGarbleReader(r io.Reader, passwd []byte) *GarbleReader {
	return &GarbleReader{r: r, passwd: passwd}
}

func (g *GarbleReader) Read(dst []byte) (int, error) {
	n, err := g.r.Read(dst)
	if len(g.passwd) > 0 {
		for i := 0; i < n; i++ {
			dst[i] ^= g.passwd[g.pos]
			g.pos++
			if g.pos >= len(g.passwd) {
				g.pos = 0
			}
		}
	}
	return n, err
}
