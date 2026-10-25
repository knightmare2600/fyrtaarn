package redfish

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// ServeLocalISO starts a short-lived HTTP file server exposing the local
// file at path, and returns a URL the BMC at bmcHost can fetch it from.
// Redfish VirtualMedia InsertMedia always works this way — the BMC pulls
// the image itself from a URL, it has no endpoint that accepts a direct
// upload from this tool.
//
// The returned stop func closes the listener. Some BMCs stream the image
// on demand for as long as the media stays mounted rather than copying it
// once at insert time, so callers must not call stop until the caller knows
// the media no longer needs to be readable — on eject, on mounting a
// different image, or on app exit — not merely once Insert returns.
func ServeLocalISO(path, bmcHost string) (url string, stop func(), err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("%s is a directory, not an ISO file", path)
	}

	localIP, err := outboundIP(bmcHost)
	if err != nil {
		return "", nil, fmt.Errorf("could not determine which local address reaches %s: %w", bmcHost, err)
	}

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", nil, fmt.Errorf("could not open a local port to serve the ISO: %w", err)
	}

	// Random path segment: this file server has no auth, so anything else
	// on the BMC's network segment that guesses the URL before the caller
	// calls stop could read the file. Serving it at the bare port root
	// would make that trivial.
	route := "/" + randomToken() + "/" + filepath.Base(path)

	mux := http.NewServeMux()
	mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	})
	srv := &http.Server{Handler: mux}

	go func() {
		_ = srv.Serve(ln)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	url = fmt.Sprintf("http://%s:%d%s", localIP, port, route)

	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		_ = srv.Close()
	}

	return url, stop, nil
}

// outboundIP returns the local address this host would use to reach remote,
// without sending any packets — a UDP "connection" only resolves a route
// via the local routing table, it never touches the network.
func outboundIP(remote string) (string, error) {
	conn, err := net.Dial("udp", net.JoinHostPort(remote, "80"))
	if err != nil {
		return "", err
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected local address type %T", conn.LocalAddr())
	}
	return addr.IP.String(), nil
}

func randomToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
