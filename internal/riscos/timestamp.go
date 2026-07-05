package riscos

import "time"

const riscosEpochOffset = 0x83AA7E80

// FileTime converts RISC OS load/exec centisecond timestamp pair to
// a Go time.Time. The RISC OS epoch is 1 January 1900.
func FileTime(load, exec uint32) time.Time {
	cs := (uint64(load&0xff) << 32) | uint64(exec)
	unix := int64(cs/100) - riscosEpochOffset
	return time.Unix(unix, 0)
}
