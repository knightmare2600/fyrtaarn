// Package iusb speaks AMI's IUSB virtual-media protocol, the one the Java
// JViewer console uses to present a local ISO image to the managed server as
// a USB CD-ROM. It is how pre-Redfish AMI MegaRAC SP BMCs (ASPEED AST2300
// era, e.g. Quanta S2RQ/S2RS boards) do virtual media.
//
// The BMC connects nothing itself: the client opens TCP to the BMC's CD port
// (5120 by default), authenticates with the KVM token the BMC issues in its
// JViewer launch file, then answers the host's SCSI commands, which the BMC
// relays one frame at a time. The image never leaves this machine; sectors
// are read on demand, so there is no size limit.
//
// Sources: the frame layout, opcodes and auth packet come from disassembling
// the BMC's own JViewer.jar (com.ami.iusb.*, javap -c) on the Quanta in
// fyrtaarn's Tested Hardware table. The response-length offset, big-endian
// CDB fields and the READ TOC allocation-length rule were cross-checked
// against the protocol notes of james-crowley/ansible-collection-asmb8-ikvm
// (GPL-3.0), which booted real ISOs on an AST2400 MegaRAC. No code was
// copied from either.
package iusb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Frame layout. All header integers are little-endian.
const (
	headerLen = 32
	signature = "IUSB    " // "IUSB" and four spaces

	offChecksum  = 11
	offDataLen   = 12
	offDevType   = 17
	offProtocol  = 18
	offDirection = 19
	offInstance  = 23
	offSeq       = 24

	devTypeCDROM = 5 // the JViewer client uses 5 for every device class
	dirToBMC     = 0x80

	// Payload (after the header) offsets of the SCSI envelope.
	envXferLen = 0  // u32 LE: requested length; response: bytes appended
	envOpcode  = 9  // CDB byte 0, or an AMI control opcode >= 0xF0
	envCDB     = 9  // 12-byte CDB
	envRespLen = 25 // u32 LE: the length the BMC forwards to the host
	envStatus  = 30 // connection status in the ACK; token type in auth
	envText    = 31 // token (auth) or other client's IP (ACK)

	opAck  = 0xF1
	opAuth = 0xF2
	opKill = 0xF6

	authPayloadLen = 128 // web-session token form, as JViewer sends
	connAccepted   = 1

	maxFrame = 1 << 20 // the BMC asks for at most 64 sectors (128 KiB)
)

// ErrInUse is returned by Dial when another client already holds the BMC's
// virtual CD-ROM.
type ErrInUse struct{ By string }

func (e ErrInUse) Error() string {
	if e.By != "" {
		return "virtual CD-ROM already in use by " + e.By
	}
	return "virtual CD-ROM already in use"
}

type header [headerLen]byte

func newHeader(dataLen int, instance byte, seq uint32) header {
	var h header
	copy(h[:], signature)
	h[8] = 1 // major
	h[9] = 0 // minor
	h[10] = headerLen
	binary.LittleEndian.PutUint32(h[offDataLen:], uint32(dataLen))
	h[offDevType] = devTypeCDROM
	h[offProtocol] = 1
	h[offDirection] = dirToBMC
	h[offInstance] = instance
	binary.LittleEndian.PutUint32(h[offSeq:], seq)
	var sum byte
	for _, b := range h {
		sum += b
	}
	h[offChecksum] = -sum // header bytes sum to 0 mod 256
	return h
}

// readFrame reads one frame and returns its header and payload.
func readFrame(r io.Reader) (header, []byte, error) {
	var h header
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return h, nil, err
	}
	if string(h[:4]) != "IUSB" {
		return h, nil, fmt.Errorf("iusb: bad frame signature %q", h[:8])
	}
	n := binary.LittleEndian.Uint32(h[offDataLen:])
	if n > maxFrame {
		return h, nil, fmt.Errorf("iusb: oversized frame (%d bytes)", n)
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return h, nil, err
	}
	return h, p, nil
}

// Device is the kind of virtual drive a session redirects; the BMC tells
// them apart by port (JViewer's -cdport, -fdport, -hdport).
type Device int

const (
	CDROM Device = iota
	Floppy
	HardDisk // also USB keys
)

func (d Device) String() string {
	switch d {
	case Floppy:
		return "floppy"
	case HardDisk:
		return "hard disk"
	}
	return "CD-ROM"
}

// authFrame builds the session-token auth packet. JViewer puts the device
// number in header byte 23 for CD-ROM and floppy (CDROMRedir, FloppyRedir)
// but byte 22 for hard disks (HarddiskRedir.SendAuth_SessionToken).
func authFrame(token string, dev Device, num byte) []byte {
	p := make([]byte, authPayloadLen)
	p[envOpcode] = opAuth
	p[envStatus] = 0 // token type: web session
	copy(p[envText:], token)
	h := newHeader(len(p), 0, 0)
	if dev == HardDisk {
		h[22] = num
	} else {
		h[offInstance] = num
	}
	h = newHeaderFrom(h)
	return append(h[:], p...)
}

// newHeaderFrom recomputes h's checksum after a field was changed.
func newHeaderFrom(h header) header {
	h[offChecksum] = 0
	var sum byte
	for _, b := range h {
		sum += b
	}
	h[offChecksum] = -sum
	return h
}

// Session is an authenticated connection to one BMC virtual drive.
type Session struct {
	conn net.Conn
	dev  Device
}

// Dial connects to the BMC's CD port and authenticates with token.
func Dial(addr, token string, instance byte) (*Session, error) {
	return DialDevice(addr, token, CDROM, instance)
}

// DialDevice connects to the port for dev and authenticates with token.
func DialDevice(addr, token string, dev Device, num byte) (*Session, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return NewSession(conn, token, dev, num)
}

// NewSession authenticates over an established connection: plain TCP, or
// TLS for the BMC's secure media ports. JViewer's secure mode is TLS that
// accepts any certificate around the same IUSB stream
// (PacketMaster.connectVmediaSSl). The connection is closed on failure.
func NewSession(conn net.Conn, token string, dev Device, num byte) (*Session, error) {
	s := &Session{conn: conn, dev: dev}
	if err := s.auth(token, num); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) auth(token string, num byte) error {
	_ = s.conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer func() { _ = s.conn.SetDeadline(time.Time{}) }()
	if _, err := s.conn.Write(authFrame(token, s.dev, num)); err != nil {
		return err
	}
	_, p, err := readFrame(s.conn)
	if err != nil {
		return fmt.Errorf("iusb: no reply to authentication: %w", err)
	}
	if len(p) <= envStatus || p[envOpcode] != opAck {
		return fmt.Errorf("iusb: unexpected reply to authentication (%d bytes)", len(p))
	}
	if p[envStatus] != connAccepted {
		var by string
		if len(p) > envText {
			by = strings.TrimSpace(strings.TrimRight(string(p[envText:]), "\x00"))
		}
		if by != "" {
			return ErrInUse{By: by}
		}
		return fmt.Errorf("iusb: BMC refused the %s session (status %d): token rejected or device in use", s.dev, p[envStatus])
	}
	return nil
}

// Close ends the redirection; the BMC sees the virtual CD-ROM disconnect.
func (s *Session) Close() error { return s.conn.Close() }

// Stats is a snapshot of what the host has read.
type Stats struct {
	Commands int
	Bytes    int64
}

// ServeCD answers the BMC's SCSI requests from img until the BMC ends the
// redirection (nil), the connection closes, or an error occurs. onCmd, if
// set, is called after each command with running totals.
func (s *Session) ServeCD(img io.ReaderAt, size int64, onCmd func(Stats)) error {
	cd := &cdrom{img: img, size: size}
	var st Stats
	for {
		h, p, err := readFrame(s.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if len(p) <= envOpcode {
			continue
		}
		if p[envOpcode] == opKill {
			return nil
		}
		data := cd.handle(p)
		st.Commands++
		st.Bytes += int64(len(data))
		if err := s.respond(h, p, data); err != nil {
			return err
		}
		if onCmd != nil {
			onCmd(st)
		}
	}
}

// respond echoes the request envelope with the data appended and its length
// in both length fields; offset 25 is the one the BMC forwards to the host.
func (s *Session) respond(req header, env, data []byte) error {
	return s.respondStatus(req, env, data, nil, nil)
}

// respondStatus is respond with a SCSI error in the status bytes and/or
// extra bytes after the data that the length fields do not count (the
// drive type JViewer appends to its 0xF4 answer).
func (s *Session) respondStatus(req header, env, data []byte, sn *sense, extra []byte) error {
	p := make([]byte, len(env)+len(data), len(env)+len(data)+len(extra))
	copy(p, env)
	copy(p[len(env):], data)
	if len(p) >= envXferLen+4 {
		binary.LittleEndian.PutUint32(p[envXferLen:], uint32(len(data)))
	}
	if len(p) >= envRespLen+4 {
		binary.LittleEndian.PutUint32(p[envRespLen:], uint32(len(data)))
	}
	if len(p) > envOverallStatus+3 {
		if sn != nil {
			p[envOverallStatus], p[envOverallStatus+1], p[envOverallStatus+2], p[envOverallStatus+3] = 1, sn.key, sn.asc, sn.ascq
		} else {
			p[envOverallStatus], p[envOverallStatus+1], p[envOverallStatus+2], p[envOverallStatus+3] = 0, 0, 0, 0
		}
	}
	p = append(p, extra...)
	h := newHeader(len(p), req[offInstance], binary.LittleEndian.Uint32(req[offSeq:]))
	// Echo the request's device fields (number, interface, client data,
	// instance: bytes 20-23); hard disks carry their number in byte 22.
	copy(h[20:24], req[20:24])
	h = newHeaderFrom(h)
	_, err := s.conn.Write(append(h[:], p...))
	return err
}

// ServeDisk answers a floppy or hard-disk session's SCSI requests from img,
// read-only, until the BMC ends it or the connection closes (see disk.go).
func (s *Session) ServeDisk(img io.ReaderAt, size int64, onCmd func(Stats)) error {
	d := &disk{img: img, size: size}
	var st Stats
	for {
		h, p, err := readFrame(s.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if len(p) <= envOpcode {
			continue
		}
		switch p[envOpcode] {
		case opKill:
			return nil
		case opSetHarddiskType:
			// JViewer's native handler fails this as an unknown opcode;
			// HarddiskRedir.run then appends the drive type byte.
			var extra []byte
			if s.dev == HardDisk {
				extra = []byte{driveTypeImage}
			}
			if err := s.respondStatus(h, p, nil, senseInvalidOpcode, extra); err != nil {
				return err
			}
			continue
		}
		data, sn := d.handle(p)
		st.Commands++
		st.Bytes += int64(len(data))
		if err := s.respondStatus(h, p, data, sn, nil); err != nil {
			return err
		}
		if onCmd != nil {
			onCmd(st)
		}
	}
}
