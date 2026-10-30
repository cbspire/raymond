package install

import (
	"io"
	"os"
	"testing"
)

func TestMissingLaunchdServiceProbeIsQuiet(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr }()
	e = probeNative("launchctl", "print", "system/dev.raymond-deliberately-missing-test")
	os.Stdout, os.Stderr = originalOut, originalErr
	w.Close()
	output, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if e == nil {
		t.Fatal("expected the service to be absent")
	}
	if len(output) != 0 {
		t.Fatalf("expected missing-service check confused the user: %s", output)
	}
}
