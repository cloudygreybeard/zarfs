package compress

import (
	"encoding/binary"
	"io"
)

const spEOF = 256

type huffNode struct {
	left  int16
	right int16
}

// HuffReader decompresses Huffman-encoded data (PackSqueeze).
type HuffReader struct {
	r     io.Reader
	tree  []huffNode
	curin int
	bpos  int
	atEOF bool
	built bool
}

// NewHuffReader creates a Huffman decompressor reading from r.
// The Huffman tree is read from the stream on first read.
func NewHuffReader(r io.Reader) *HuffReader {
	return &HuffReader{
		r:    r,
		bpos: 99,
	}
}

func (h *HuffReader) readTree() error {
	if h.built {
		return nil
	}
	var numnodes int16
	if err := binary.Read(h.r, binary.LittleEndian, &numnodes); err != nil {
		return err
	}
	h.tree = make([]huffNode, numnodes)
	for i := int16(0); i < numnodes; i++ {
		if err := binary.Read(h.r, binary.LittleEndian, &h.tree[i].left); err != nil {
			return err
		}
		if err := binary.Read(h.r, binary.LittleEndian, &h.tree[i].right); err != nil {
			return err
		}
	}
	h.built = true
	return nil
}

func (h *HuffReader) getChild(node int, direction int) int {
	if direction&1 == 0 {
		return int(h.tree[node].left)
	}
	return int(h.tree[node].right)
}

func (h *HuffReader) gethuff() (int, error) {
	if h.atEOF {
		return -1, io.EOF
	}
	i := 0
	for {
		h.bpos++
		if h.bpos > 7 {
			var b [1]byte
			if _, err := h.r.Read(b[:]); err != nil {
				return -1, err
			}
			h.curin = int(b[0])
			h.bpos = 0
			i = h.getChild(i, h.curin)
		} else {
			h.curin >>= 1
			i = h.getChild(i, h.curin)
		}
		if i < 0 {
			break
		}
	}

	i = -(i + 1)
	if i == spEOF {
		h.atEOF = true
		return -1, io.EOF
	}
	return i, nil
}

func (h *HuffReader) Read(dst []byte) (int, error) {
	if err := h.readTree(); err != nil {
		return 0, err
	}
	n := 0
	for n < len(dst) {
		b, err := h.gethuff()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		dst[n] = byte(b)
		n++
	}
	return n, nil
}
