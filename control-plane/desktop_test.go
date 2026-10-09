package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDesktopLifecycleProtocol(t *testing.T) {
	var out bytes.Buffer
	if err := desktopReady(&out, "127.0.0.1:12345"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"api_base_url":"http://127.0.0.1:12345"`) {
		t.Fatal(out.String())
	}
	for _, input := range []string{"", "invalid\n{\"command\":\"shutdown\"}\n", "{\"command\":\"unknown\"}\n"} {
		cancelled := false
		desktopInput(strings.NewReader(input), func() { cancelled = true })
		if !cancelled {
			t.Fatal("desktop shutdown did not cancel")
		}
	}
}
