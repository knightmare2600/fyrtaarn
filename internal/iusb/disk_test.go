package iusb

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// diskReq sends one request and returns the response payload (after the
// envelope) and the envelope's status bytes.
func (b *fakeBMC) diskReq(cdb ...byte) (data []byte, status [4]byte, hdr header) {
	b.t.Helper()
	env := make([]byte, 30)
	copy(env[envCDB:], cdb)
	b.seq++
	h := newHeader(len(env), 0, b.seq)
	h[22] = 3 // hard-disk device number, as the BMC would echo it
	h = newHeaderFrom(h)
	if _, err := b.conn.Write(append(h[:], env...)); err != nil {
		b.t.Fatal(err)
	}
	rh, p, err := readFrame(b.conn)
	if err != nil {
		b.t.Fatal(err)
	}
	copy(status[:], p[envOverallStatus:envOverallStatus+4])
	return p[len(env):], status, rh
}

func startDiskSession(t *testing.T, img []byte) (*fakeBMC, chan error) {
	t.Helper()
	srv, cli := net.Pipe()
	s := &Session{conn: cli, dev: HardDisk}
	done := make(chan error, 1)
	go func() { done <- s.ServeDisk(bytes.NewReader(img), int64(len(img)), nil) }()
	b := &fakeBMC{t: t, conn: srv}
	t.Cleanup(func() { _ = srv.Close() })
	return b, done
}

func TestServeDisk(t *testing.T) {
	img := make([]byte, 4*diskBlock+10) // 5 blocks, last one partial
	for i := range img {
		img[i] = byte(i/diskBlock) + 1
	}
	b, done := startDiskSession(t, img)
	ok := [4]byte{}

	if d, st, hdr := b.diskReq(scsiTestUnitReady); len(d) != 0 || st != ok || hdr[22] != 3 {
		t.Fatalf("TUR data %d status %v devnum %d", len(d), st, hdr[22])
	}
	if d, st, _ := b.diskReq(scsiReadCapacity); st != ok || !bytes.Equal(d, []byte{0, 0, 0, 4, 0, 0, 2, 0}) {
		t.Fatalf("READ CAPACITY % x", d)
	}
	d, st, _ := b.diskReq(scsiRead10, 0, 0, 0, 0, 1, 0, 0, 2) // LBA 1, 2 blocks
	if st != ok || len(d) != 2*diskBlock || d[0] != 2 || d[diskBlock] != 3 {
		t.Fatalf("READ(10) %d bytes, [0]=%d", len(d), d[0])
	}
	if _, st, _ := b.diskReq(scsiRead10, 0, 0, 0, 0, 4, 0, 0, 2); st != [4]byte{1, 5, 0x21, 0} {
		t.Fatalf("read past end status %v", st)
	}
	// Read-only: MODE SENSE reports write protection, WRITE is refused.
	if d, _, _ := b.diskReq(scsiModeSense6, 0, 0x3F, 0, 0xFF); !bytes.Equal(d, []byte{3, 0, 0x80, 0}) {
		t.Fatalf("MODE SENSE(6) % x", d)
	}
	if d, _, _ := b.diskReq(scsiModeSense10, 0, 0x3F, 0, 0, 0, 0, 0, 8); len(d) != 8 || d[3] != 0x80 {
		t.Fatalf("MODE SENSE(10) % x", d)
	}
	if _, st, _ := b.diskReq(scsiWrite10, 0, 0, 0, 0, 0, 0, 0, 1); st != [4]byte{1, 7, 0x27, 0} {
		t.Fatalf("WRITE status %v", st)
	}
	if d, _, _ := b.diskReq(scsiReadFormatCaps, 0, 0, 0, 0, 0, 0, 0, 12); len(d) != 12 || binary.BigEndian.Uint32(d[4:]) != 5 || d[8] != 2 {
		t.Fatalf("READ FORMAT CAPACITIES % x", d)
	}
	if _, st, _ := b.diskReq(0x12); st != [4]byte{1, 5, 0x20, 0} { // INQUIRY: the BMC answers it, not us
		t.Fatalf("unknown opcode status %v", st)
	}
	// 0xF4: invalid-opcode status, no counted data, drive type appended.
	d, st, _ = b.diskReq(opSetHarddiskType)
	if st != [4]byte{1, 5, 0x20, 0} || !bytes.Equal(d, []byte{driveTypeImage}) {
		t.Fatalf("0xF4 status %v data % x", st, d)
	}

	kill := make([]byte, 30)
	kill[envOpcode] = opKill
	h := newHeader(len(kill), 0, 99)
	_, _ = b.conn.Write(append(h[:], kill...))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAuthFrameDeviceNumber(t *testing.T) {
	for _, c := range []struct {
		dev Device
		at  int
	}{{CDROM, 23}, {Floppy, 23}, {HardDisk, 22}} {
		f := authFrame("tok", c.dev, 7)
		var sum byte
		for _, x := range f[:headerLen] {
			sum += x
		}
		if f[c.at] != 7 || sum != 0 {
			t.Errorf("%v: byte %d = %d, header sum %d", c.dev, c.at, f[c.at], sum)
		}
	}
}
