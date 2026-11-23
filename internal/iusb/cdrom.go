package iusb

import (
	"encoding/binary"
	"io"
)

// cdrom answers the SCSI/MMC commands the BMC relays from the host. The
// BMC firmware itself answers INQUIRY; JViewer's native handler implements
// TEST UNIT READY, START STOP UNIT, READ CAPACITY(10), READ(10), READ TOC
// and READ(12), and these are what arrive here. Multi-byte CDB and response
// fields are big-endian, as in all SCSI.
type cdrom struct {
	img  io.ReaderAt
	size int64
}

const blockSize = 2048

const (
	scsiTestUnitReady = 0x00
	scsiStartStop     = 0x1B
	scsiReadCapacity  = 0x25
	scsiRead10        = 0x28
	scsiReadTOC       = 0x43
	scsiRead12        = 0xA8

	maxBlocksPerRead = 64 // JViewer's MAX_READ_SECTORS
)

func (c *cdrom) blocks() uint32 { return uint32((c.size + blockSize - 1) / blockSize) }

// handle returns the data-in bytes for one request (nil for status-only
// commands and anything unrecognised, which JViewer also answers with no
// data).
func (c *cdrom) handle(env []byte) []byte {
	if len(env) < envCDB+12 {
		return nil
	}
	cdb := env[envCDB : envCDB+12]
	switch cdb[0] {
	case scsiReadCapacity:
		out := make([]byte, 8)
		last := c.blocks()
		if last > 0 {
			last--
		}
		binary.BigEndian.PutUint32(out, last)
		binary.BigEndian.PutUint32(out[4:], blockSize)
		return out
	case scsiRead10:
		return c.read(binary.BigEndian.Uint32(cdb[2:]), uint32(binary.BigEndian.Uint16(cdb[7:])))
	case scsiRead12:
		return c.read(binary.BigEndian.Uint32(cdb[2:]), binary.BigEndian.Uint32(cdb[6:]))
	case scsiReadTOC:
		return c.toc(cdb)
	}
	return nil
}

// read returns blocks*2048 bytes from lba, zero-filled past the end of the
// image. Requests are clamped to JViewer's 64-sector maximum so a corrupt
// CDB cannot make us allocate gigabytes.
func (c *cdrom) read(lba, n uint32) []byte {
	if n > maxBlocksPerRead {
		n = maxBlocksPerRead
	}
	out := make([]byte, int(n)*blockSize)
	off := int64(lba) * blockSize
	if off < c.size {
		_, _ = c.img.ReadAt(out, off) // short read at EOF leaves zeros
	}
	return out
}

// toc builds READ TOC/PMA/ATIP for a single data track: format 0 (TOC) or
// 1 (session info), in LBA or MSF addressing, truncated to the allocation
// length while the header still states the full length, as SCSI requires.
// A host that is sent more than it allocated misreads the TOC and decides
// there is no medium (observed with a Linux installer by the asmb8 project).
func (c *cdrom) toc(cdb []byte) []byte {
	msf := cdb[1]&0x02 != 0
	format := cdb[2] & 0x0F
	alloc := int(binary.BigEndian.Uint16(cdb[7:]))

	desc := func(track byte, lba uint32) []byte {
		d := make([]byte, 8)
		d[1] = 0x14 // ADR 1, control: data track
		d[2] = track
		if msf {
			f := lba + 150 // MSF addresses include the 2-second pregap
			d[5], d[6], d[7] = byte(f/(75*60)), byte(f/75%60), byte(f%75)
		} else {
			binary.BigEndian.PutUint32(d[4:], lba)
		}
		return d
	}

	var body []byte
	if format == 1 {
		body = desc(1, 0) // first track of the last session
	} else {
		body = append(desc(1, 0), desc(0xAA, c.blocks())...) // track 1, lead-out
	}
	out := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint16(out, uint16(len(body)+2))
	out[2], out[3] = 1, 1 // first and last track (or session)
	out = append(out, body...)
	if alloc > 0 && alloc < len(out) {
		out = out[:alloc]
	}
	return out
}
