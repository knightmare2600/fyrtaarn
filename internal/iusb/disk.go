package iusb

import (
	"encoding/binary"
	"io"
)

// Floppy and hard-disk (USB key) image redirection: the same IUSB framing
// as the CD-ROM, on the BMC's floppy and hard-disk ports. JViewer's native
// handler for hard-disk images (ExecuteHarddiskImageSCSICmd in
// libjavaharddiskwrapper.so, disassembled from the Quanta's JViewer) answers
// TEST UNIT READY, FORMAT UNIT, MODE SENSE(6/10), START STOP UNIT, PREVENT
// ALLOW MEDIUM REMOVAL, READ FORMAT CAPACITIES, READ CAPACITY(10), READ(10),
// WRITE(10) and READ(12), with 512-byte blocks and at most 256 sectors per
// read (HarddiskRedir.MAX_READ_SECTORS). Errors go in the envelope's status
// bytes (SetErrorStatus): overall status 1, then sense key, ASC, ASCQ.
//
// Images are served read-only: MODE SENSE reports write protection, and
// WRITE/FORMAT fail with sense 7/27h (write protected), the code the native
// handler uses for the same case.

const (
	diskBlock         = 512
	maxDiskSectors    = 256
	envOverallStatus  = 21 // then sense key, ASC, ASCQ at 22-24
	opSetHarddiskType = 0xF4

	scsiFormatUnit          = 0x04
	scsiModeSense6          = 0x1A
	scsiPreventAllow        = 0x1E
	scsiReadFormatCaps      = 0x23
	scsiWrite10             = 0x2A
	scsiModeSense10         = 0x5A
	driveTypeImage     byte = 0x80 // JViewer sends -128 for an image file
)

// sense is a SCSI error for the envelope's status bytes.
type sense struct{ key, asc, ascq byte }

var (
	senseInvalidOpcode  = &sense{0x05, 0x20, 0x00}
	senseLBAOutOfRange  = &sense{0x05, 0x21, 0x00}
	senseWriteProtected = &sense{0x07, 0x27, 0x00}
)

type disk struct {
	img  io.ReaderAt
	size int64
}

func (d *disk) blocks() uint32 { return uint32((d.size + diskBlock - 1) / diskBlock) }

// handle returns the data-in bytes and, for a failed command, its sense.
func (d *disk) handle(env []byte) ([]byte, *sense) {
	if len(env) < envCDB+12 {
		return nil, senseInvalidOpcode
	}
	cdb := env[envCDB : envCDB+12]
	switch cdb[0] {
	case scsiTestUnitReady, scsiStartStop, scsiPreventAllow:
		return nil, nil
	case scsiReadCapacity:
		out := make([]byte, 8)
		binary.BigEndian.PutUint32(out, max(d.blocks(), 1)-1)
		binary.BigEndian.PutUint32(out[4:], diskBlock)
		return out, nil
	case scsiReadFormatCaps:
		// Capacity list header, then one "formatted media" descriptor.
		out := make([]byte, 12)
		out[3] = 8
		binary.BigEndian.PutUint32(out[4:], d.blocks())
		out[8] = 0x02 // formatted media
		out[9], out[10], out[11] = 0, diskBlock>>8, diskBlock&0xFF
		return truncate(out, int(binary.BigEndian.Uint16(cdb[7:]))), nil
	case scsiModeSense6:
		// Header only: data length, medium type, device-specific (WP), no
		// block descriptors.
		return truncate([]byte{3, 0, 0x80, 0}, int(cdb[4])), nil
	case scsiModeSense10:
		return truncate([]byte{0, 6, 0, 0x80, 0, 0, 0, 0}, int(binary.BigEndian.Uint16(cdb[7:]))), nil
	case scsiRead10:
		return d.read(binary.BigEndian.Uint32(cdb[2:]), uint32(binary.BigEndian.Uint16(cdb[7:])))
	case scsiRead12:
		return d.read(binary.BigEndian.Uint32(cdb[2:]), binary.BigEndian.Uint32(cdb[6:]))
	case scsiWrite10, scsiFormatUnit:
		return nil, senseWriteProtected
	}
	return nil, senseInvalidOpcode
}

func (d *disk) read(lba, n uint32) ([]byte, *sense) {
	if uint64(lba)+uint64(n) > uint64(d.blocks()) {
		return nil, senseLBAOutOfRange
	}
	n = min(n, maxDiskSectors)
	out := make([]byte, int(n)*diskBlock)
	_, _ = d.img.ReadAt(out, int64(lba)*diskBlock) // short read at EOF leaves zeros
	return out, nil
}

func truncate(b []byte, alloc int) []byte {
	if alloc > 0 && alloc < len(b) {
		return b[:alloc]
	}
	return b
}
