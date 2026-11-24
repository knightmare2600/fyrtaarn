package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

func TestFlags(t *testing.T) {
	for _, c := range []struct {
		args    []string
		code    int
		out     string
		backend string
	}{
		{[]string{"-h"}, 0, "Usage: fyrtaarn", ""},
		{[]string{"--help"}, 0, "Usage: fyrtaarn", ""},
		{[]string{"-v"}, 0, "Fyrtaarn dev (none)", ""},
		{[]string{"--version"}, 0, "IPMI library", ""},
		{[]string{"-n"}, -1, "", "native"},
		{[]string{"--native"}, -1, "", "native"},
		{[]string{"-i"}, -1, "", "ipmitool"},
		{[]string{}, -1, "", ""},
		{[]string{"-n", "-i"}, 2, "cannot be used together", ""},
		{[]string{"-x"}, 2, "flag provided but not defined", ""},
		{[]string{"extra"}, 2, "unexpected argument", ""},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			t.Setenv(ipmi.BackendEnv, "")
			var out, errb bytes.Buffer
			if code := run(c.args, &out, &errb); code != c.code {
				t.Fatalf("code %d, want %d (out %q, err %q)", code, c.code, out.String(), errb.String())
			}
			if c.out != "" && !strings.Contains(out.String()+errb.String(), c.out) {
				t.Errorf("output %q lacks %q", out.String()+errb.String(), c.out)
			}
			if got := os.Getenv(ipmi.BackendEnv); got != c.backend {
				t.Errorf("%s=%q, want %q", ipmi.BackendEnv, got, c.backend)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	oldLook, oldVer := lookPath, toolVersion
	t.Cleanup(func() { lookPath, toolVersion = oldLook, oldVer })
	toolVersion = func(string, ...string) string { return "x 1.0" }

	lookPath = func(f string) (string, error) { return "/usr/bin/" + f, nil }
	var out bytes.Buffer
	if code := check(&out); code != 0 || !strings.Contains(out.String(), "[ok] nmap") || !strings.Contains(out.String(), "[ok] ipmitool") {
		t.Fatalf("all present: %d\n%s", code, out.String())
	}

	// No ipmitool: still fine. No nmap: exit 1.
	lookPath = func(f string) (string, error) {
		if f == "ipmitool" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + f, nil
	}
	out.Reset()
	if code := check(&out); code != 0 || !strings.Contains(out.String(), "optional") {
		t.Fatalf("no ipmitool: %d\n%s", code, out.String())
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	out.Reset()
	if code := check(&out); code != 1 || !strings.Contains(out.String(), "[!!] nmap") {
		t.Fatalf("no nmap: %d\n%s", code, out.String())
	}
}
