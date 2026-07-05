package compress

import "io"

const lzwZEOF = 257

// LZWZooReader decompresses PackDir/Zoo variant LZW data.
// This variant differs from ncompress: after a CLEAR code, the first
// code is a literal byte. An EOF code (257) signals end of stream.
type LZWZooReader struct {
	r          io.Reader
	maxbits    int
	eos        bool
	gotHeader  bool
	bits       int
	maxmaxcode int
	freeEnt    int
	oldCode    int
	inchar     int
	hash       map[int]*hashEntry

	outBuf []byte
	outPos int

	// Bit reader state (byte-aligned reads).
	bitBuf    uint64
	bitsAvail int
}

// NewLZWZooReader creates a PackDir/Zoo LZW decompressor.
// maxbits is the maximum code width (typically header value + 12).
func NewLZWZooReader(r io.Reader, maxbits int) *LZWZooReader {
	zr := &LZWZooReader{
		r:       r,
		maxbits: maxbits,
		hash:    make(map[int]*hashEntry, 256),
		freeEnt: lzwFirst,
	}
	for i := 0; i < 256; i++ {
		zr.hash[i] = &hashEntry{ch: byte(i)}
	}
	return zr
}

func (zr *LZWZooReader) readHeader() {
	if zr.gotHeader {
		return
	}
	zr.gotHeader = true
	zr.maxmaxcode = (1 << zr.maxbits) - 1
	zr.outBuf = make([]byte, 0, zr.maxmaxcode)
	zr.bits = 9
	zr.freeEnt = lzwFirst + 1
	zr.oldCode = 0
}

func (zr *LZWZooReader) readPackDirBitField() (int, error) {
	for zr.bitsAvail < zr.bits {
		var b [1]byte
		_, err := zr.r.Read(b[:])
		if err != nil {
			return -1, err
		}
		zr.bitBuf |= uint64(b[0]) << zr.bitsAvail
		zr.bitsAvail += 8
		zr.bitBuf &= (1 << zr.bitsAvail) - 1
	}
	mask := uint64((1 << zr.bits) - 1)
	code := int(zr.bitBuf & mask)
	zr.bitBuf >>= zr.bits
	zr.bitsAvail -= zr.bits
	zr.bitBuf &= (1 << zr.bitsAvail) - 1
	return code, nil
}

func (zr *LZWZooReader) readLZW(need int) error {
	if zr.eos {
		return io.EOF
	}
	zr.readHeader()

	stack := make([]byte, 65535)

	for len(zr.outBuf)-zr.outPos < need {
		code, err := zr.readPackDirBitField()
		if code == -1 || err != nil {
			zr.eos = true
			break
		}

		if code == lzwZEOF {
			zr.eos = true
			break
		}

		if code == lzwClear {
			for c := 0; c < lzwClear; c++ {
				zr.hash[c].code = 0
			}
			zr.bits = 9
			zr.freeEnt = lzwFirst + 1

			nc, err := zr.readPackDirBitField()
			if nc == -1 || err != nil {
				zr.eos = true
				break
			}
			zr.inchar = nc
			zr.oldCode = nc
			zr.outBuf = append(zr.outBuf, byte(nc))
			continue
		}

		incode := code
		si := 0

		if code >= zr.freeEnt {
			stack[si] = byte(zr.inchar)
			si++
			code = zr.oldCode
		}

		for code >= lzwClear {
			e := zr.hash[code]
			stack[si] = e.ch
			si++
			code = e.code
		}

		zr.inchar = int(zr.hash[code].ch)
		stack[si] = byte(zr.inchar)
		si++

		for si > 0 {
			si--
			zr.outBuf = append(zr.outBuf, stack[si])
		}

		if zr.freeEnt < zr.maxmaxcode {
			zr.hash[zr.freeEnt] = &hashEntry{ch: byte(zr.inchar), code: zr.oldCode}
			zr.freeEnt++
			if zr.freeEnt > (1<<zr.bits)-1 {
				zr.bits++
			}
		}
		zr.oldCode = incode
	}

	if len(zr.outBuf)-zr.outPos == 0 {
		return io.EOF
	}
	return nil
}

func (zr *LZWZooReader) Read(dst []byte) (int, error) {
	if zr.eos && zr.outPos >= len(zr.outBuf) {
		return 0, io.EOF
	}

	if len(zr.outBuf)-zr.outPos < len(dst) {
		if err := zr.readLZW(len(dst)); err != nil && zr.outPos >= len(zr.outBuf) {
			return 0, err
		}
	}

	avail := len(zr.outBuf) - zr.outPos
	n := len(dst)
	if n > avail {
		n = avail
	}
	copy(dst[:n], zr.outBuf[zr.outPos:zr.outPos+n])
	zr.outPos += n

	if zr.outPos > 4096 {
		remaining := len(zr.outBuf) - zr.outPos
		copy(zr.outBuf, zr.outBuf[zr.outPos:])
		zr.outBuf = zr.outBuf[:remaining]
		zr.outPos = 0
	}

	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}
