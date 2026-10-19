// Command vsfbridge runs the loopback bridge; with -state it pairs peers on the operator's say-so.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/bridge"
	"github.com/insightlabs38-pixel/vsf/backend/pairing"
)

func main() {
	origins := flag.String("origins", "", "comma separated exact browser origins allowed to call the bridge")
	state := flag.String("state", "", "directory for pairing records; enables operator-approved pairing instead of a static token")
	accessTTL := flag.Duration("access-ttl", 0, "lifetime of an access token; zero keeps the default")
	flag.Parse()
	var mu sync.Mutex
	enc := json.NewEncoder(os.Stderr)
	cfg := bridge.Config{
		AllowedOrigins: splitNonEmpty(*origins),
		OnRequest: func(r bridge.RequestRecord) {
			mu.Lock()
			defer mu.Unlock()
			_ = enc.Encode(r)
		},
	}
	var svc *pairing.Service
	token := ""
	if *state != "" {
		var err error
		svc, err = pairing.New(pairing.Config{Dir: *state, Peers: cfg.AllowedOrigins, AccessTTL: *accessTTL})
		if err != nil {
			fatal(err)
		}
		cfg.Pairing = svc
	} else {
		var err error
		if token, err = bridge.NewToken(); err != nil {
			fatal(err)
		}
		cfg.Token = token
	}
	srv, err := bridge.New(cfg)
	if err != nil {
		fatal(err)
	}
	addr, err := srv.Listen()
	if err != nil {
		fatal(err)
	}
	out := json.NewEncoder(os.Stdout)
	if svc == nil {
		_ = out.Encode(map[string]string{"addr": addr, "token": token})
		os.Stdout.Close()
		fmt.Fprintln(os.Stderr, srv.Serve())
		return
	}
	_ = out.Encode(map[string]string{"addr": addr, "mode": "pairing"})
	go operate(svc, out, &mu)
	fmt.Fprintln(os.Stderr, srv.Serve())
}

// operate announces pairing requests and reads the operator's decisions; nothing is granted without one.
func operate(svc *pairing.Service, out *json.Encoder, mu *sync.Mutex) {
	emit := func(v map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		_ = out.Encode(v)
	}
	go func() {
		seen := map[string]bool{}
		for range time.Tick(100 * time.Millisecond) {
			for _, p := range svc.Pending() {
				if !seen[p.ID] {
					seen[p.ID] = true
					emit(map[string]any{"event": "pair_request", "pair_id": p.ID, "peer": p.Peer})
					fmt.Fprintf(os.Stderr, "pairing request %s from %s: type \"approve %s\" or \"deny %s\"\n", p.ID, p.Peer, p.ID, p.ID)
				}
			}
		}
	}()
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		f := strings.Fields(in.Text())
		if len(f) == 0 {
			continue
		}
		arg := ""
		if len(f) > 1 {
			arg = f[1]
		}
		switch f[0] {
		case "approve":
			code, err := svc.Approve(arg)
			if err != nil {
				emit(map[string]any{"event": "error", "error": err.Error()})
				continue
			}
			emit(map[string]any{"event": "code", "pair_id": arg, "code": code[:5] + "-" + code[5:]})
		case "deny":
			svc.Deny(arg)
			emit(map[string]any{"event": "denied", "pair_id": arg})
		case "revoke":
			if err := svc.Revoke(arg); err != nil {
				emit(map[string]any{"event": "error", "error": err.Error()})
				continue
			}
			emit(map[string]any{"event": "revoked", "pairing_id": arg})
		case "list":
			emit(map[string]any{"event": "pairings", "pairings": svc.List()})
		default:
			emit(map[string]any{"event": "error", "error": "unknown command"})
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
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
