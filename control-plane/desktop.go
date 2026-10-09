package main

import (
	"bufio"
	"encoding/json"
	"io"
)

// Only lifecycle metadata is written to stdout; business logs remain on stderr.
func desktopReady(w io.Writer, address string) error {
	return json.NewEncoder(w).Encode(map[string]any{
		"event": "ready", "protocol": 1, "api_base_url": "http://" + address,
	})
}

func desktopInput(r io.Reader, cancel func()) {
	defer cancel() // EOF also means the owning desktop process has disappeared.
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var message struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(scanner.Bytes(), &message) == nil && message.Command == "shutdown" {
			return
		}
	}
}
