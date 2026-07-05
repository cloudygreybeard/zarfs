// Package compress implements decompression algorithms for RISC OS archive formats.
package compress

import "io"

const (
	cfsEndBlock        = 0x00
	cfsCompressedBlock = 0x01
	cfsRawBlock        = 0x02
	cfsHeaderBlock     = 0x03
	cfsZeroBlock       = 0x04
	cfsFirstFree       = 256
	cfsMaxTokLen       = 0x4000
)

// CFSReader decompresses CFS block-based LZW data.
type CFSReader struct {
	r   io.Reader
	eos bool

	srcBuf    []byte
	srcPos    int
	srcEnd    int
	srcFenceI int

	blockBuf     []byte
	blockBufI    int
	blockBufRead int

	pfx   []int
	token []int

	bits     int
	highcode int
	shift    int

	gotHeader bool
}

// NewCFSReader wraps r with CFS block decompression.
func NewCFSReader(r io.Reader) *CFSReader {
	return &CFSReader{
		r:      r,
		srcBuf: make([]byte, 0x4000),
		token:  make([]int, cfsMaxTokLen),
	}
}

func (c *CFSReader) readHeader() {
	if c.gotHeader {
		return
	}
	c.gotHeader = true
	maxmaxcode := (1 << 12) - 1
	c.pfx = make([]int, maxmaxcode+1)
	c.bits = 9
}

func (c *CFSReader) fillSrc() error {
	if c.srcPos > 0 {
		copy(c.srcBuf, c.srcBuf[c.srcPos:c.srcEnd])
		c.srcEnd -= c.srcPos
		c.srcFenceI -= c.srcPos
		c.srcPos = 0
	}
	n, err := c.r.Read(c.srcBuf[c.srcEnd:])
	if n > 0 {
		c.srcEnd += n
	}
	if n == 0 && err != nil {
		return err
	}
	return nil
}

func (c *CFSReader) getChar() (int, error) {
	if c.srcPos == c.srcEnd {
		if err := c.fillSrc(); err != nil {
			return -1, err
		}
	}
	if c.srcPos == c.srcEnd {
		return -1, io.EOF
	}
	ch := int(c.srcBuf[c.srcPos]) & 0xff
	c.srcPos++
	return ch, nil
}

func (c *CFSReader) nextcode() (int, error) {
	i := c.srcPos
	diff := c.shift + c.bits

	if (c.srcEnd-i)<<3 < diff {
		if err := c.fillSrc(); err != nil && c.srcEnd-c.srcPos == 0 {
			return -1, err
		}
		i = c.srcPos
		if (c.srcEnd-i)<<3 < diff {
			return -1, io.EOF
		}
	}

	var code int
	diff -= 16
	if diff > 0 {
		b0, err := c.getChar()
		if err != nil {
			return -1, err
		}
		b1, err := c.getChar()
		if err != nil {
			return -1, err
		}
		if c.srcPos >= c.srcEnd {
			return -1, io.EOF
		}
		b2 := int(c.srcBuf[c.srcPos])
		code = b0 | (b1 << 8) | (b2 << 16)
		code >>= c.shift
		c.shift = diff
	} else {
		b0, err := c.getChar()
		if err != nil {
			return -1, err
		}
		if c.srcPos >= c.srcEnd {
			return -1, io.EOF
		}
		b1 := int(c.srcBuf[c.srcPos])
		code = (b0 | (b1 << 8)) & 0xffff
		code >>= c.shift
		c.shift = 8 + diff
		if c.shift == 0 {
			c.srcPos++
		}
	}

	return code & c.highcode, nil
}

func (c *CFSReader) setBitSize(bits int, start bool) error {
	if start {
		if c.srcPos > c.srcFenceI {
			if c.shift > 0 {
				c.srcPos++
			}
			s := c.srcPos - c.srcFenceI
			s = (s + 3) &^ 3
			c.srcPos = c.srcFenceI + s
		}
		c.shift = 0

		bt, err := c.getChar()
		if err != nil || bt == -1 {
			c.blockBuf = nil
			return io.EOF
		}

		cl0, err := c.getChar()
		if err != nil {
			return io.EOF
		}
		cl1, err := c.getChar()
		if err != nil {
			return io.EOF
		}
		cl2, err := c.getChar()
		if err != nil {
			return io.EOF
		}
		if cl0 == -1 || cl1 == -1 || cl2 == -1 {
			c.blockBuf = nil
			return io.EOF
		}

		codelimit := cl0 | (cl1 << 8) | (cl2 << 16)
		if bt == cfsCompressedBlock {
			codelimit += 0xff
		}

		c.blockBuf = make([]byte, codelimit)
		c.blockBufI = 0
		c.blockBufRead = 0
		c.srcFenceI = c.srcPos

		switch bt {
		case cfsCompressedBlock:
			return c.decompressBlock(codelimit)
		case cfsRawBlock:
			return c.copyRawBlock(codelimit)
		case cfsZeroBlock:
			return c.copyZeroBlock(codelimit)
		case cfsEndBlock:
			return io.EOF
		default:
			return io.EOF
		}
	}
	c.shift++
	c.bits = bits
	return nil
}

func (c *CFSReader) decompressBlock(codelimit int) error {
	c.highcode = (1 << 9) - 1
	c.bits = 9
	nextfree := cfsFirstFree

	prefixcode, err := c.nextcode()
	if err != nil || prefixcode < 0 {
		return io.EOF
	}
	sufxchar := prefixcode
	c.blockBuf[c.blockBufI] = byte(sufxchar)
	c.blockBufI++

	for nextfree < codelimit {
		savecode, err := c.nextcode()
		if err != nil || savecode < 0 {
			break
		}
		code := savecode

		p := cfsMaxTokLen - 1
		q := p

		if code >= nextfree {
			if code != nextfree {
				return io.EOF
			}
			code = prefixcode
			c.token[p] = sufxchar
			p--
		}
		for code >= 256 {
			code = c.pfx[code]
			c.token[p] = code
			p--
			code >>= 8
		}
		sufxchar = code
		c.token[p] = sufxchar
		p--

		n := q - p
		for i := 0; i < n; i++ {
			if c.blockBufI >= len(c.blockBuf) {
				nb := make([]byte, len(c.blockBuf)*2)
				copy(nb, c.blockBuf)
				c.blockBuf = nb
			}
			c.blockBuf[c.blockBufI] = byte(c.token[p+1+i])
			c.blockBufI++
		}

		c.pfx[nextfree] = (prefixcode << 8) | sufxchar
		prefixcode = savecode
		if nextfree == c.highcode {
			c.bits++
			if err := c.setBitSize(c.bits, false); err != nil {
				return err
			}
			c.highcode += nextfree + 1
		}
		nextfree++
	}
	return nil
}

func (c *CFSReader) copyRawBlock(codelimit int) error {
	if c.srcEnd-c.srcPos < codelimit {
		if err := c.fillSrc(); err != nil && c.srcEnd-c.srcPos == 0 {
			return err
		}
	}
	n := codelimit
	if n > c.srcEnd-c.srcPos {
		n = c.srcEnd - c.srcPos
	}
	copy(c.blockBuf[c.blockBufI:], c.srcBuf[c.srcPos:c.srcPos+n])
	c.blockBufI += n
	c.srcPos += n
	return nil
}

func (c *CFSReader) copyZeroBlock(codelimit int) error {
	for i := c.blockBufI; i < c.blockBufI+codelimit && i < len(c.blockBuf); i++ {
		c.blockBuf[i] = 0
	}
	c.blockBufI += codelimit
	if c.blockBufI > len(c.blockBuf) {
		c.blockBufI = len(c.blockBuf)
	}
	return nil
}

func (c *CFSReader) Read(dst []byte) (int, error) {
	if c.eos {
		return 0, io.EOF
	}
	c.readHeader()

	total := 0
	remaining := len(dst)

	for remaining > 0 {
		avail := c.blockBufI - c.blockBufRead
		if avail > 0 {
			n := avail
			if n > remaining {
				n = remaining
			}
			copy(dst[total:], c.blockBuf[c.blockBufRead:c.blockBufRead+n])
			c.blockBufRead += n
			total += n
			remaining -= n
		}

		if remaining <= 0 {
			break
		}

		c.blockBufI = 0
		c.blockBufRead = 0
		if err := c.setBitSize(9, true); err != nil {
			c.eos = true
			if total > 0 {
				return total, nil
			}
			return 0, io.EOF
		}
	}

	return total, nil
}
