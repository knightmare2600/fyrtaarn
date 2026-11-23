package iusb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

// fakeBMC is the server side of the protocol, driven step by step by a test.
type fakeBMC struct {
	t    *testing.T
	conn net.Conn
	seq  uint32
}

// startSession runs Dial's handshake against a fake BMC that replies to the
// auth frame with status, then returns both ends.
func startSession(t *testing.T, status byte, otherIP string) (*Session, *fakeBMC, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	bmcCh := make(chan *fakeBMC, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			bmcCh <- nil
			return
		}
		b := &fakeBMC{t: t, conn: c}
		h, p, err := readFrame(c)
		if err != nil || p[envOpcode] != opAuth || len(p) != authPayloadLen || h[offDirection] != dirToBMC {
			t.Errorf("auth frame: err=%v len=%d op=%#x", err, len(p), p[envOpcode])
		}
		if got := string(bytes.TrimRight(p[envText:], "\x00")); got != "tok123" {
			t.Errorf("token %q", got)
		}
		ack := make([]byte, 64)
		ack[envOpcode] = opAck
		ack[envStatus] = status
		copy(ack[envText:], otherIP)
		hh := newHeader(len(ack), 0, 0)
		_, _ = c.Write(append(hh[:], ack...))
		bmcCh <- b
	}()
	s, err := Dial(ln.Addr().String(), "tok123", 0)
	b := <-bmcCh
	if b != nil {
		t.Cleanup(func() { _ = b.conn.Close() })
	}
	return s, b, err
}

// cmd sends one SCSI request and returns the response payload's data part.
func (b *fakeBMC) cmd(cdb ...byte) []byte {
	b.t.Helper()
	env := make([]byte, 30)
	copy(env[envCDB:], cdb)
	b.seq++
	h := newHeader(len(env), 0, b.seq)
	h[offDirection] = 0 // BMC to client
	if _, err := b.conn.Write(append(h[:], env...)); err != nil {
		b.t.Fatal(err)
	}
	rh, p, err := readFrame(b.conn)
	if err != nil {
		b.t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(rh[offSeq:]); got != b.seq {
		b.t.Fatalf("response seq %d, want %d", got, b.seq)
	}
	var sum byte
	for _, x := range rh {
		sum += x
	}
	if sum != 0 || rh[offDirection] != dirToBMC || rh[offDevType] != devTypeCDROM {
		b.t.Fatalf("response header %x", rh)
	}
	data := p[len(env):]
	if n := binary.LittleEndian.Uint32(p[envRespLen:]); int(n) != len(data) {
		b.t.Fatalf("resp len field %d, data %d", n, len(data))
	}
	if n := binary.LittleEndian.Uint32(p[envXferLen:]); int(n) != len(data) {
		b.t.Fatalf("xfer len field %d, data %d", n, len(data))
	}
	if !bytes.Equal(p[envCDB:envCDB+12], env[envCDB:envCDB+12]) {
		b.t.Fatal("envelope not echoed")
	}
	return data
}

func TestServeCD(t *testing.T) {
	// 5 full sectors plus a partial one: 6 blocks, last LBA 5.
	img := make([]byte, 5*blockSize+100)
	for i := range img {
		img[i] = byte(i/blockSize) + 1
	}
	s, b, err := startSession(t, connAccepted, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	var last Stats
	go func() {
		done <- s.ServeCD(bytes.NewReader(img), int64(len(img)), func(st Stats) { last = st })
	}()

	if d := b.cmd(scsiTestUnitReady); len(d) != 0 {
		t.Fatalf("TUR returned %d bytes", len(d))
	}
	if d := b.cmd(scsiReadCapacity); !bytes.Equal(d, []byte{0, 0, 0, 5, 0, 0, 8, 0}) {
		t.Fatalf("READ CAPACITY % x", d)
	}
	d := b.cmd(scsiRead10, 0, 0, 0, 0, 2, 0, 0, 2) // LBA 2, 2 blocks
	if len(d) != 2*blockSize || d[0] != 3 || d[blockSize] != 4 {
		t.Fatalf("READ(10) len %d first %d second %d", len(d), d[0], d[blockSize])
	}
	d = b.cmd(scsiRead12, 0, 0, 0, 0, 5, 0, 0, 0, 1) // LBA 5, 1 block: 100 bytes then zeros
	if len(d) != blockSize || d[0] != 6 || d[99] != 6 || d[100] != 0 {
		t.Fatalf("READ(12) tail len %d [0]=%d [99]=%d [100]=%d", len(d), d[0], d[99], d[100])
	}
	if d := b.cmd(scsiRead10, 0, 0, 0, 0, 9, 0, 0, 1); len(d) != blockSize || d[0] != 0 {
		t.Fatal("read past end not zero-filled")
	}
	if d := b.cmd(scsiRead10, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF); len(d) != maxBlocksPerRead*blockSize {
		t.Fatalf("oversized read not clamped: %d", len(d))
	}
	// TOC, LBA form, full: header + track 1 + lead-out at LBA 6.
	toc := b.cmd(scsiReadTOC, 0, 0, 0, 0, 0, 0, 0, 100)
	want := []byte{0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 0, 0, 0, 0x14, 0xAA, 0, 0, 0, 0, 6}
	if !bytes.Equal(toc, want) {
		t.Fatalf("TOC\n got % x\nwant % x", toc, want)
	}
	// Allocation length 12: truncated, header length still 18.
	if toc := b.cmd(scsiReadTOC, 0, 0, 0, 0, 0, 0, 0, 12); !bytes.Equal(toc, want[:12]) {
		t.Fatalf("truncated TOC % x", toc)
	}
	// MSF: lead-out at LBA 6 is 6+150 = 156 frames = 00:02:06.
	toc = b.cmd(scsiReadTOC, 0x02, 0, 0, 0, 0, 0, 0, 100)
	if !bytes.Equal(toc[16:20], []byte{0, 0, 2, 6}) || !bytes.Equal(toc[8:12], []byte{0, 0, 2, 0}) {
		t.Fatalf("MSF TOC % x", toc)
	}
	// Session info (format 1).
	if toc := b.cmd(scsiReadTOC, 0, 1, 0, 0, 0, 0, 0, 100); len(toc) != 12 || toc[1] != 10 {
		t.Fatalf("session info % x", toc)
	}
	if d := b.cmd(scsiStartStop, 0, 0, 0, 2); len(d) != 0 {
		t.Fatal("START STOP returned data")
	}

	// Kill ends ServeCD cleanly.
	kill := make([]byte, 30)
	kill[envOpcode] = opKill
	h := newHeader(len(kill), 0, 99)
	_, _ = b.conn.Write(append(h[:], kill...))
	if err := <-done; err != nil {
		t.Fatalf("ServeCD: %v", err)
	}
	if last.Commands != 11 {
		t.Fatalf("stats %+v", last)
	}
}

func TestDialInUse(t *testing.T) {
	_, _, err := startSession(t, 5, "10.1.2.3")
	var inUse ErrInUse
	if !errors.As(err, &inUse) || inUse.By != "10.1.2.3" {
		t.Fatalf("err = %v", err)
	}
	_, _, err = startSession(t, 0, "")
	if err == nil {
		t.Fatal("rejected auth accepted")
	}
}

func TestServeCDClosed(t *testing.T) {
	s, b, err := startSession(t, connAccepted, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = b.conn.Close()
	if err := s.ServeCD(bytes.NewReader(nil), 0, nil); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ServeCD after BMC close: %v", err)
	}
}
