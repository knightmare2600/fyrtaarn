package iusb

import (
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/legacytls"
)

// TestSessionOverTLS runs the IUSB handshake and a command inside TLS, as
// on a BMC whose media redirection is set to secure.
func TestSessionOverTLS(t *testing.T) {
	certSrv := httptest.NewTLSServer(http.NotFoundHandler()) // for its test certificate
	defer certSrv.Close()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: certSrv.TLS.Certificates})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	bmcDone := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, p, err := readFrame(c) // auth
		if err != nil || p[envOpcode] != opAuth {
			t.Errorf("auth over TLS: %v", err)
			return
		}
		ack := make([]byte, 64)
		ack[envOpcode], ack[envStatus] = opAck, connAccepted
		h := newHeader(len(ack), 0, 0)
		_, _ = c.Write(append(h[:], ack...))
		env := make([]byte, 30)
		env[envCDB] = scsiReadCapacity
		h = newHeader(len(env), 0, 1)
		_, _ = c.Write(append(h[:], env...))
		_, rp, err := readFrame(c)
		if err != nil {
			t.Error(err)
			return
		}
		bmcDone <- rp[len(env):]
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := legacytls.Dial(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(conn, "tok", CDROM, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.ServeCD(bytes.NewReader(make([]byte, 4*blockSize)), 4*blockSize, nil) }()
	select {
	case d := <-bmcDone:
		if !bytes.Equal(d, []byte{0, 0, 0, 3, 0, 0, 8, 0}) {
			t.Fatalf("READ CAPACITY over TLS % x", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no response over TLS")
	}
	_ = s.Close()
}
