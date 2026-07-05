package compress

import "io"

const runMark = 0x90

// PackReader decompresses RISC OS Pack (RLE) compressed data.
type PackReader struct {
	r        io.Reader
	buf      []byte
	pos      int
	end      int
	lastByte byte
	eof      bool
}

// NewPackReader wraps r with Pack RLE decompression.
func NewPackReader(r io.Reader) *PackReader {
	return &PackReader{
		r:   r,
		buf: make([]byte, 1026),
	}
}

func (p *PackReader) fill() error {
	if p.eof {
		return io.EOF
	}
	n, err := p.r.Read(p.buf[:1024])
	if n == 0 {
		p.eof = true
		return io.EOF
	}
	p.end = n

	// Ensure run markers are not split across buffers.
	if p.buf[p.end-1] == runMark {
		extra := make([]byte, 1)
		m, _ := p.r.Read(extra)
		if m == 1 {
			p.buf[p.end] = extra[0]
			p.end++
		}
	} else {
		peek := make([]byte, 1)
		m, _ := p.r.Read(peek)
		if m == 1 {
			if peek[0] == runMark {
				p.buf[p.end] = runMark
				p.end++
				extra := make([]byte, 1)
				m2, _ := p.r.Read(extra)
				if m2 == 1 {
					p.buf[p.end] = extra[0]
					p.end++
				}
			} else {
				// Push the byte back by shifting buffer; we use a
				// simple approach: store it for next fill.
				p.buf[p.end] = peek[0]
				p.end++
			}
		}
	}
	if err != nil && err != io.EOF {
		return err
	}
	p.pos = 0
	return nil
}

func (p *PackReader) Read(dst []byte) (int, error) {
	wrote := 0
	for wrote < len(dst) {
		if p.pos >= p.end {
			if err := p.fill(); err != nil {
				if wrote > 0 {
					return wrote, nil
				}
				return 0, err
			}
		}

		if p.buf[p.pos] == runMark {
			if p.pos+1 >= p.end {
				if wrote > 0 {
					return wrote, nil
				}
				return 0, io.ErrUnexpectedEOF
			}
			if p.buf[p.pos+1] == 0 {
				dst[wrote] = runMark
				p.lastByte = runMark
				p.pos += 2
				wrote++
			} else {
				rb := p.lastByte
				if p.pos > 0 && p.buf[p.pos-1] != runMark {
					rb = p.buf[p.pos-1]
				}
				cnt := int(p.buf[p.pos+1])
				for cnt > 1 && wrote < len(dst) {
					dst[wrote] = rb
					wrote++
					cnt--
				}
				p.buf[p.pos+1] = byte(cnt)
				if cnt <= 1 {
					p.pos += 2
				}
			}
		} else {
			p.lastByte = p.buf[p.pos]
			dst[wrote] = p.buf[p.pos]
			wrote++
			p.pos++
		}
	}
	return wrote, nil
}
