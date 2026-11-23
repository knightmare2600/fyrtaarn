package legacytls

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // RC4-MD5 is the point of this file
	"crypto/rand"
	"crypto/rc4" //nolint:gosec // as above
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // TLS 1.0/1.1 PRF and RC4-SHA MAC
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/big"
	"net"
	"sync"
	"time"
)

// The fallback client: RFC 2246 (TLS 1.0) / RFC 4346 (TLS 1.1), RSA key
// exchange, RC4 stream cipher only. Stream ciphers have no IV or padding, so
// the record layer is identical in 1.0 and 1.1, and both versions use the
// same MD5/SHA-1 PRF. No extensions are sent (some old stacks abort on any),
// renegotiation is refused and session resumption is not offered.

const (
	suiteRC4MD5 uint16 = 0x0004 // TLS_RSA_WITH_RC4_128_MD5
	suiteRC4SHA uint16 = 0x0005 // TLS_RSA_WITH_RC4_128_SHA

	recCCS   = 20
	recAlert = 21
	recHS    = 22
	recData  = 23

	hsHelloRequest = 0
	hsClientHello  = 1
	hsServerHello  = 2
	hsCertificate  = 11
	hsServerDone   = 14
	hsClientKX     = 16
	hsFinished     = 20

	maxPlaintext = 16384
)

func u24(b []byte) int { return int(b[0])<<16 | int(b[1])<<8 | int(b[2]) }

// pHash is P_hash from RFC 2246 section 5.
func pHash(h func() hash.Hash, secret, seed []byte, n int) []byte {
	out := make([]byte, 0, n+64)
	m := hmac.New(h, secret)
	m.Write(seed)
	a := m.Sum(nil)
	for len(out) < n {
		m.Reset()
		m.Write(a)
		m.Write(seed)
		out = m.Sum(out)
		m.Reset()
		m.Write(a)
		a = m.Sum(nil)
	}
	return out[:n]
}

// prf10 is the TLS 1.0/1.1 PRF: P_MD5 over the first half of the secret
// XOR P_SHA1 over the second half, the halves overlapping by one byte when
// the length is odd (RFC 2246 section 5).
func prf10(secret []byte, label string, seed []byte, n int) []byte {
	half := (len(secret) + 1) / 2
	ls := append([]byte(label), seed...)
	a := pHash(md5.New, secret[:half], ls, n)
	b := pHash(sha1.New, secret[len(secret)-half:], ls, n)
	for i := range a {
		a[i] ^= b[i]
	}
	return a
}

// keys derives the master secret and the key block for an RC4 suite:
// client MAC, server MAC, client key, server key (RFC 2246 section 6.3).
func keys(pms, cr, sr []byte, macLen int) (master, cMAC, sMAC, cKey, sKey []byte) {
	master = prf10(pms, "master secret", append(append([]byte{}, cr...), sr...), 48)
	kb := prf10(master, "key expansion", append(append([]byte{}, sr...), cr...), 2*macLen+32)
	return master, kb[:macLen], kb[macLen : 2*macLen], kb[2*macLen : 2*macLen+16], kb[2*macLen+16:]
}

type halfConn struct {
	ciph *rc4.Cipher
	mac  hash.Hash
	seq  uint64
}

func (h *halfConn) macOf(typ byte, vers uint16, data []byte) []byte {
	var hdr [13]byte
	binary.BigEndian.PutUint64(hdr[:8], h.seq)
	hdr[8] = typ
	binary.BigEndian.PutUint16(hdr[9:], vers)
	binary.BigEndian.PutUint16(hdr[11:], uint16(len(data)))
	h.mac.Reset()
	h.mac.Write(hdr[:])
	h.mac.Write(data)
	h.seq++
	return h.mac.Sum(nil)
}

type rc4Conn struct {
	net.Conn
	vers  uint16
	suite uint16

	rmu  sync.Mutex
	in   *halfConn
	rbuf []byte // decrypted application data not yet returned
	pend []byte // handshake bytes not yet parsed
	rerr error  // sticky read error

	wmu sync.Mutex
	out *halfConn

	hs bytes.Buffer // handshake transcript, for Finished
}

func (c *rc4Conn) Info() Info { return Info{Version: c.vers, Suite: c.suite, Fallback: true} }

func (c *rc4Conn) writeRecord(typ byte, data []byte) error {
	vers := c.vers
	if vers == 0 {
		vers = tls.VersionTLS10 // record version before ServerHello
	}
	if c.out != nil {
		mac := c.out.macOf(typ, vers, data)
		buf := append(append(make([]byte, 0, len(data)+len(mac)), data...), mac...)
		c.out.ciph.XORKeyStream(buf, buf)
		data = buf
	}
	rec := make([]byte, 5, 5+len(data))
	rec[0] = typ
	binary.BigEndian.PutUint16(rec[1:], vers)
	binary.BigEndian.PutUint16(rec[3:], uint16(len(data)))
	_, err := c.Conn.Write(append(rec, data...))
	return err
}

func (c *rc4Conn) readRecord() (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(c.Conn, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[3:]))
	if n > maxPlaintext+2048 {
		return 0, nil, fmt.Errorf("legacytls: oversized record (%d bytes)", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, data); err != nil {
		return 0, nil, err
	}
	if c.in != nil {
		c.in.ciph.XORKeyStream(data, data)
		ml := c.in.mac.Size()
		if len(data) < ml {
			return 0, nil, errors.New("legacytls: short record")
		}
		body, mac := data[:len(data)-ml], data[len(data)-ml:]
		if !hmac.Equal(mac, c.in.macOf(hdr[0], c.vers, body)) {
			return 0, nil, errors.New("legacytls: bad record MAC")
		}
		data = body
	}
	if hdr[0] == recAlert {
		if len(data) >= 2 && data[1] == 0 {
			return 0, nil, io.EOF // close_notify
		}
		if len(data) >= 2 {
			return 0, nil, fmt.Errorf("legacytls: remote alert %d", data[1])
		}
		return 0, nil, errors.New("legacytls: malformed alert")
	}
	return hdr[0], data, nil
}

// readHS returns the next handshake message, header included.
func (c *rc4Conn) readHS() ([]byte, error) {
	for len(c.pend) < 4 || len(c.pend) < 4+u24(c.pend[1:]) {
		typ, data, err := c.readRecord()
		if err != nil {
			return nil, err
		}
		if typ != recHS {
			return nil, fmt.Errorf("legacytls: unexpected record type %d during handshake", typ)
		}
		c.pend = append(c.pend, data...)
	}
	n := 4 + u24(c.pend[1:])
	msg := c.pend[:n:n]
	c.pend = c.pend[n:]
	return msg, nil
}

func (c *rc4Conn) sendHS(typ byte, body []byte) error {
	msg := append([]byte{typ, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	c.hs.Write(msg)
	return c.writeRecord(recHS, msg)
}

func (c *rc4Conn) verifyData(master []byte, label string) []byte {
	m := md5.Sum(c.hs.Bytes())
	s := sha1.Sum(c.hs.Bytes())
	return prf10(master, label, append(m[:], s[:]...), 12)
}

// rsaEncrypt is PKCS #1 v1.5 type 2 encryption done with math/big, because
// crypto/rsa refuses keys under 1024 bits and old BMCs may carry 512-bit
// ones.
func rsaEncrypt(pub *rsa.PublicKey, msg []byte) ([]byte, error) {
	k := (pub.N.BitLen() + 7) / 8
	if len(msg) > k-11 {
		return nil, errors.New("legacytls: RSA key too small")
	}
	em := make([]byte, k)
	em[1] = 2
	ps := em[2 : k-len(msg)-1]
	if _, err := rand.Read(ps); err != nil {
		return nil, err
	}
	for i := range ps {
		for ps[i] == 0 {
			if _, err := rand.Read(ps[i : i+1]); err != nil {
				return nil, err
			}
		}
	}
	copy(em[k-len(msg):], msg)
	c := new(big.Int).Exp(new(big.Int).SetBytes(em), big.NewInt(int64(pub.E)), pub.N)
	return c.FillBytes(make([]byte, k)), nil
}

func parseServerHello(body []byte) (vers uint16, random []byte, suite uint16, err error) {
	if len(body) < 38 {
		return 0, nil, 0, errors.New("legacytls: short ServerHello")
	}
	sid := int(body[34])
	if len(body) < 35+sid+3 {
		return 0, nil, 0, errors.New("legacytls: short ServerHello")
	}
	return binary.BigEndian.Uint16(body), body[2:34], binary.BigEndian.Uint16(body[35+sid:]), nil
}

func parseCertificate(body []byte) (*rsa.PublicKey, error) {
	if len(body) < 6 {
		return nil, errors.New("legacytls: short Certificate")
	}
	l := u24(body[3:])
	if len(body) < 6+l {
		return nil, errors.New("legacytls: short Certificate")
	}
	cert, err := x509.ParseCertificate(body[6 : 6+l])
	if err != nil {
		return nil, fmt.Errorf("legacytls: server certificate: %w", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("legacytls: server certificate key is not RSA")
	}
	return pub, nil
}

// client runs the handshake over raw. It offers TLS 1.1 (the server may pick
// 1.0) and both RC4 suites, MD5 first since that is the one Go cannot do.
func client(raw net.Conn) (*rc4Conn, error) {
	c := &rc4Conn{Conn: raw}
	const offered = tls.VersionTLS11

	cr := make([]byte, 32)
	if _, err := rand.Read(cr); err != nil {
		return nil, err
	}
	hello := binary.BigEndian.AppendUint16(nil, offered)
	hello = append(hello, cr...)
	hello = append(hello, 0)    // no session ID
	hello = append(hello, 0, 4) // two cipher suites
	hello = binary.BigEndian.AppendUint16(hello, suiteRC4MD5)
	hello = binary.BigEndian.AppendUint16(hello, suiteRC4SHA)
	hello = append(hello, 1, 0) // compression: null only
	if err := c.sendHS(hsClientHello, hello); err != nil {
		return nil, err
	}

	var sr []byte
	var pub *rsa.PublicKey
	for done := false; !done; {
		msg, err := c.readHS()
		if err != nil {
			return nil, err
		}
		body := msg[4:]
		switch msg[0] {
		case hsServerHello:
			c.hs.Write(msg)
			if c.vers, sr, c.suite, err = parseServerHello(body); err != nil {
				return nil, err
			}
			if c.vers != tls.VersionTLS10 && c.vers != tls.VersionTLS11 {
				return nil, fmt.Errorf("legacytls: server chose unsupported version %04x", c.vers)
			}
			if c.suite != suiteRC4MD5 && c.suite != suiteRC4SHA {
				return nil, fmt.Errorf("legacytls: server chose unoffered suite %04x", c.suite)
			}
		case hsCertificate:
			c.hs.Write(msg)
			if pub, err = parseCertificate(body); err != nil {
				return nil, err
			}
		case hsServerDone:
			c.hs.Write(msg)
			done = true
		default:
			// ServerKeyExchange (export suites, DHE) and CertificateRequest
			// are for suites or modes this client never offers.
			return nil, fmt.Errorf("legacytls: unsupported handshake message %d", msg[0])
		}
	}
	if sr == nil || pub == nil {
		return nil, errors.New("legacytls: server skipped ServerHello or Certificate")
	}

	// The pre-master secret carries the version offered, not the one chosen
	// (RFC 4346 section 7.4.7.1).
	pms := make([]byte, 48)
	binary.BigEndian.PutUint16(pms, offered)
	if _, err := rand.Read(pms[2:]); err != nil {
		return nil, err
	}
	enc, err := rsaEncrypt(pub, pms)
	if err != nil {
		return nil, err
	}
	if err := c.sendHS(hsClientKX, append(binary.BigEndian.AppendUint16(nil, uint16(len(enc))), enc...)); err != nil {
		return nil, err
	}

	newMAC := md5.New
	if c.suite == suiteRC4SHA {
		newMAC = sha1.New
	}
	master, cMAC, sMAC, cKey, sKey := keys(pms, cr, sr, newMAC().Size())
	cc, _ := rc4.NewCipher(cKey)
	sc, _ := rc4.NewCipher(sKey)

	if err := c.writeRecord(recCCS, []byte{1}); err != nil {
		return nil, err
	}
	c.out = &halfConn{ciph: cc, mac: hmac.New(newMAC, cMAC)}
	if err := c.sendHS(hsFinished, c.verifyData(master, "client finished")); err != nil {
		return nil, err
	}

	typ, data, err := c.readRecord()
	if err != nil {
		return nil, err
	}
	if typ != recCCS || len(data) != 1 || data[0] != 1 {
		return nil, errors.New("legacytls: expected ChangeCipherSpec")
	}
	c.in = &halfConn{ciph: sc, mac: hmac.New(newMAC, sMAC)}
	want := c.verifyData(master, "server finished")
	msg, err := c.readHS()
	if err != nil {
		return nil, err
	}
	if msg[0] != hsFinished || !hmac.Equal(msg[4:], want) {
		return nil, errors.New("legacytls: server Finished does not verify")
	}
	return c, nil
}

func (c *rc4Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.rbuf) == 0 {
		if c.rerr != nil {
			return 0, c.rerr
		}
		typ, data, err := c.readRecord()
		if err != nil {
			c.rerr = err
			return 0, err
		}
		switch typ {
		case recData:
			c.rbuf = data
		case recHS:
			// A HelloRequest asks for renegotiation, which is refused by
			// ignoring it (RFC 2246 section 7.4.1.1 allows that). Anything
			// else here is a protocol error.
			if len(data) < 4 || data[0] != hsHelloRequest {
				c.rerr = errors.New("legacytls: unexpected handshake message after handshake")
				return 0, c.rerr
			}
		default:
			c.rerr = fmt.Errorf("legacytls: unexpected record type %d", typ)
			return 0, c.rerr
		}
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func (c *rc4Conn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	for off := 0; off < len(p); off += maxPlaintext {
		end := min(off+maxPlaintext, len(p))
		if err := c.writeRecord(recData, p[off:end]); err != nil {
			return off, err
		}
	}
	return len(p), nil
}

// Close sends close_notify, best effort, then closes the socket. It skips
// the alert rather than wait behind a Write blocked on a stalled peer.
func (c *rc4Conn) Close() error {
	if c.wmu.TryLock() {
		_ = c.Conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = c.writeRecord(recAlert, []byte{1, 0})
		c.wmu.Unlock()
	}
	return c.Conn.Close()
}
