package compress

import (
	"fmt"
	"io"
)

// LZW type constants matching the Java LZWConstants.
const (
	Compress     = iota // Headerless LZW; reads maxbits from first byte
	Squash              // 13-bit fixed, no header
	Crunch              // 12-bit default, reads maxbits from first byte if not preset
	UnixCompress        // Standard Unix compress with 1f 9d header
)

const (
	lzwClear   = 256
	lzwFirst   = 257
	squashBits = 13
)

type hashEntry struct {
	ch   byte
	code int
}

// NcompressReader decompresses ncompress-style LZW data.
type NcompressReader struct {
	r          io.Reader
	typ        int
	maxbits    int
	blockMode  bool
	gotHeader  bool
	eos        bool
	bits       int
	maxcode    int
	maxmaxcode int
	freeEnt    int
	oldCode    int
	finchar    int
	hash       map[int]*hashEntry

	outBuf []byte
	outPos int

	// Bit reader state.
	bitBuf        uint64
	bitsAvail     int
	byteBuf       []byte
	byteBufPos    int
	byteBufLen    int
	bitReaderSize int
}

// NewNcompressReader creates an ncompress LZW decompressor.
// typ is one of UnixCompress, Squash, or Crunch.
// maxbits is used for Crunch (caller-supplied); ignored for others.
func NewNcompressReader(r io.Reader, typ, maxbits int) *NcompressReader {
	nr := &NcompressReader{
		r:       r,
		typ:     typ,
		maxbits: maxbits,
		hash:    make(map[int]*hashEntry, 256),
		oldCode: -1,
		finchar: -1,
		freeEnt: lzwFirst,
		byteBuf: make([]byte, 20),
	}
	for i := 0; i < 256; i++ {
		nr.hash[i] = &hashEntry{ch: byte(i)}
	}
	return nr
}

func (nr *NcompressReader) readHeader() error {
	if nr.gotHeader {
		return nil
	}
	nr.gotHeader = true

	switch nr.typ {
	case UnixCompress:
		hdr := make([]byte, 3)
		if _, err := io.ReadFull(nr.r, hdr); err != nil {
			return fmt.Errorf("reading compress header: %w", err)
		}
		if hdr[0] != 0x1f || hdr[1] != 0x9d {
			return fmt.Errorf("bad compress magic %02x %02x", hdr[0], hdr[1])
		}
		nr.maxbits = int(hdr[2] & 0x1f)
		nr.blockMode = hdr[2]&0x80 != 0
	case Squash:
		nr.maxbits = squashBits
		nr.blockMode = true
	case Crunch, Compress:
		if nr.maxbits == 0 {
			b := make([]byte, 1)
			if _, err := io.ReadFull(nr.r, b); err != nil {
				return fmt.Errorf("reading lzw maxbits: %w", err)
			}
			nr.maxbits = int(b[0])
		}
		nr.blockMode = true
	}

	nr.maxmaxcode = 1 << nr.maxbits
	nr.outBuf = make([]byte, 0, nr.maxmaxcode)
	nr.bits = 9
	nr.maxcode = (1 << 9) - 1
	nr.setBitSize(9)
	return nil
}

func (nr *NcompressReader) readBitField() (int, error) {
	for nr.bitsAvail < nr.bits {
		if nr.byteBufPos >= nr.bits {
			// ncompress reads in blocks of nr.bits bytes. Partial
			// reads at EOF are accepted (returns what's available).
			n, err := nr.r.Read(nr.byteBuf[:nr.bits])
			if n == 0 {
				if err != nil {
					return -1, err
				}
				return -1, io.EOF
			}
			nr.bitsAvail = 0
			nr.byteBufPos = 0
			nr.byteBufLen = n
			nr.bitBuf = 0
		}
		if nr.byteBufPos >= nr.byteBufLen {
			return -1, io.EOF
		}
		b := uint64(nr.byteBuf[nr.byteBufPos]) & 0xff
		nr.byteBufPos++
		nr.bitBuf |= b << nr.bitsAvail
		nr.bitsAvail += 8
	}

	code := int(nr.bitBuf & ((1 << nr.bits) - 1))
	nr.bitBuf >>= nr.bits
	nr.bitsAvail -= nr.bits
	return code, nil
}

func (nr *NcompressReader) setBitSize(bits int) {
	if nr.bitReaderSize != bits {
		nr.bitReaderSize = bits
		nr.byteBufPos = bits
	}
}

func (nr *NcompressReader) clearTable() {
	for code := 0; code < 256; code++ {
		e := nr.hash[code]
		e.code = code
	}
}

func (nr *NcompressReader) readLZW(need int) (bool, error) {
	if nr.eos {
		return false, io.EOF
	}
	if err := nr.readHeader(); err != nil {
		return false, err
	}

	stack := make([]byte, 65535)

	for len(nr.outBuf)-nr.outPos < need {
		if nr.freeEnt > nr.maxcode {
			nr.bits++
			if nr.bits == nr.maxbits {
				nr.maxcode = nr.maxmaxcode
			} else {
				nr.maxcode = (1 << nr.bits) - 1
			}
			nr.setBitSize(nr.bits)
			continue
		}

		code, err := nr.readBitField()
		if code == -1 || err != nil {
			nr.eos = true
			break
		}

		if nr.oldCode == -1 {
			nr.finchar = code
			nr.oldCode = code
			nr.outBuf = append(nr.outBuf, byte(code))
			continue
		}

		if code == lzwClear && nr.blockMode {
			nr.clearTable()
			nr.bits = 9
			nr.maxcode = (1 << 9) - 1
			nr.setBitSize(nr.bits)
			nr.freeEnt = lzwFirst - 1
			continue
		}

		incode := code
		si := len(stack)

		if code >= nr.freeEnt {
			si--
			stack[si] = byte(nr.finchar)
			code = nr.oldCode
		}

		for code >= 256 {
			e := nr.hash[code]
			if e == nil {
				code = 0
				break
			}
			si--
			stack[si] = e.ch
			code = e.code
		}

		fe := nr.hash[code]
		if fe != nil {
			nr.finchar = int(fe.ch)
		} else {
			nr.finchar = 0
		}
		si--
		stack[si] = byte(nr.finchar)

		for i := si; i < len(stack); i++ {
			nr.outBuf = append(nr.outBuf, stack[i])
		}

		if fc := nr.freeEnt; fc < nr.maxmaxcode {
			nr.hash[fc] = &hashEntry{ch: byte(nr.finchar), code: nr.oldCode}
			nr.freeEnt++
		}
		nr.oldCode = incode
	}
	return len(nr.outBuf)-nr.outPos > 0, nil
}

func (nr *NcompressReader) Read(dst []byte) (int, error) {
	if nr.eos && nr.outPos >= len(nr.outBuf) {
		return 0, io.EOF
	}

	if len(nr.outBuf)-nr.outPos < len(dst) {
		ok, err := nr.readLZW(len(dst))
		if !ok && err != nil && nr.outPos >= len(nr.outBuf) {
			return 0, err
		}
	}

	avail := len(nr.outBuf) - nr.outPos
	n := len(dst)
	if n > avail {
		n = avail
	}
	copy(dst[:n], nr.outBuf[nr.outPos:nr.outPos+n])
	nr.outPos += n

	// Compact buffer when we've consumed a significant portion.
	if nr.outPos > 4096 {
		remaining := len(nr.outBuf) - nr.outPos
		copy(nr.outBuf, nr.outBuf[nr.outPos:])
		nr.outBuf = nr.outBuf[:remaining]
		nr.outPos = 0
	}

	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}
