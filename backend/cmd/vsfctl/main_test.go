package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 64 {
		t.Fatalf("no args: %d", code)
	}
	if code := run([]string{"bogus"}, &out, &errb); code != 64 {
		t.Fatalf("bogus: %d", code)
	}
	if code := run([]string{"verify", "/nonexistent/vsf"}, &out, &errb); code != 66 {
		t.Fatalf("missing file: %d", code)
	}
	out.Reset()
	if code := run([]string{"version"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "vsf.smoke.envelope") {
		t.Fatalf("version: %d %q", code, out.String())
	}
}
