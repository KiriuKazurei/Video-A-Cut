//go:build probe

// Probe drives the running control plane through the official MCP Go SDK
// client. Build tag keeps it out of the normal build; run with:
//
//	go run -tags probe ./internal/mcpserver/probe -url http://127.0.0.1:8789/mcp -token-env VAC_MCP_TOKEN
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct {
	token  string
	base   http.RoundTripper
	source string
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	if b.source != "" {
		r.Header.Set("X-VAC-Agent-Source", b.source)
	}
	return b.base.RoundTrip(r)
}

func main() {
	url := flag.String("url", "", "MCP endpoint")
	tokenEnv := flag.String("token-env", "", "optional internal worker token env; desktop MCP needs none")
	protocol := flag.String("protocol", "", "MCP protocol version; default SDK negotiation")
	clientName := flag.String("client-name", "vac-probe", "reported MCP client name")
	source := flag.String("source", "vac-probe", "reported Agent source recorded in human-only logs")
	script := flag.String("calls", "", "semicolon-separated tool:jsonargs list")
	flag.Parse()
	token := os.Getenv(*tokenEnv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: *clientName, Version: "0.2.3"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             *url,
		HTTPClient:           &http.Client{Transport: bearer{token: token, base: http.DefaultTransport, source: *source}},
		DisableStandaloneSSE: true,
	}, &mcp.ClientSessionOptions{ProtocolVersion: *protocol})
	if err != nil {
		fmt.Println("CONNECT_ERROR:", err)
		os.Exit(2)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		fmt.Println("LIST_ERROR:", err)
		os.Exit(2)
	}
	var names []string
	for _, t := range tools.Tools {
		names = append(names, t.Name)
	}
	fmt.Println("TOOLS:", strings.Join(names, ","))
	for _, step := range strings.Split(*script, ";") {
		if step == "" {
			continue
		}
		name, raw, _ := strings.Cut(step, ":")
		args := map[string]any{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				fmt.Println("BAD_ARGS:", step, err)
				os.Exit(2)
			}
		}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			fmt.Printf("%s => PROTOCOL_ERROR %v\n", name, err)
			continue
		}
		out, _ := json.Marshal(res.StructuredContent)
		text := ""
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				text = tc.Text
			}
		}
		if res.IsError {
			fmt.Printf("%s => ERROR %s\n", name, text)
		} else {
			fmt.Printf("%s => %s\n", name, out)
		}
	}
}
