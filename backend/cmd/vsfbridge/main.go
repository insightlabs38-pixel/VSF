// Command vsfbridge runs the loopback bridge prototype and prints its address and pairing token once.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/insightlabs38-pixel/vsf/backend/bridge"
)

func main() {
	origins := flag.String("origins", "", "comma separated exact browser origins allowed to call the bridge")
	flag.Parse()
	token, err := bridge.NewToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var mu sync.Mutex
	enc := json.NewEncoder(os.Stderr)
	srv, err := bridge.New(bridge.Config{
		Token:          token,
		AllowedOrigins: splitNonEmpty(*origins),
		OnRequest: func(r bridge.RequestRecord) {
			mu.Lock()
			defer mu.Unlock()
			_ = enc.Encode(r)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	addr, err := srv.Listen()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"addr": addr, "token": token})
	os.Stdout.Close()
	fmt.Fprintln(os.Stderr, srv.Serve())
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
