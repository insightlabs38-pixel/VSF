// Command vsfbridge runs the loopback bridge and prints its address; with --pair it also prints a one-time pairing code.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/insightlabs38-pixel/vsf/backend/bridge"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
)

func main() {
	origins := flag.String("origins", "", "comma separated exact extension origins (chrome-extension://<id>) allowed to call the bridge")
	state := flag.String("state", "", "directory holding the pairing registry; empty keeps it in memory")
	pair := flag.Bool("pair", false, "print a pairing code at start; send SIGUSR1 for another")
	native := flag.String("native", "", "path of the native validator (vsf-fabric); defaults to the one beside this program")
	port := flag.Int("port", 0, "loopback port; 0 picks a free one")
	dev := flag.Bool("dev-token", false, "also accept a static development token, printed once")
	flag.Parse()
	list := splitNonEmpty(*origins)
	var token string
	var pairing *bridge.Pairing
	var bindings *bridge.Bindings
	if *dev || len(list) == 0 {
		t, err := bridge.NewToken()
		if err != nil {
			fatal(err)
		}
		token = t
	}
	if len(list) > 0 {
		path := *native
		if path == "" {
			exe, err := os.Executable()
			if err != nil {
				fatal(err)
			}
			path = filepath.Join(filepath.Dir(exe), "vsf-fabric")
		}
		worker, err := execution.StartWorker(path)
		if err != nil {
			fatal(fmt.Errorf("the native validator is required to bind sessions: %w", err))
		}
		bindings = bridge.NewBindings(bridge.NativeValidator{Worker: worker}, nil)
		cfg := bridge.PairConfig{Origins: list, OnRevoke: bindings.RevokeClient}
		if *state != "" {
			cfg.Store = bridge.FileStore{Path: filepath.Join(*state, "pairing.json")}
		}
		p, err := bridge.NewPairing(cfg)
		if err != nil {
			fatal(err)
		}
		pairing = p
	}
	var mu sync.Mutex
	enc := json.NewEncoder(os.Stderr)
	srv, err := bridge.New(bridge.Config{
		Token:          token,
		Pairing:        pairing,
		Bindings:       bindings,
		AllowedOrigins: list,
		OnRequest: func(r bridge.RequestRecord) {
			mu.Lock()
			defer mu.Unlock()
			_ = enc.Encode(r)
		},
	})
	if err != nil {
		fatal(err)
	}
	addr, err := srv.ListenPort(*port)
	if err != nil {
		fatal(err)
	}
	out := map[string]string{"addr": addr}
	if token != "" {
		out["token"] = token
	}
	offer := func() {
		if pairing == nil {
			return
		}
		code, exp, err := pairing.NewOffer(list[0])
		if err != nil {
			fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(map[string]any{"pair_code": code, "origin": list[0], "expires_at": exp.Unix()})
	}
	if *pair && pairing != nil {
		code, exp, err := pairing.NewOffer(list[0])
		if err != nil {
			fatal(err)
		}
		out["pair_code"], out["pair_origin"] = code, list[0]
		out["pair_expires_at"] = fmt.Sprint(exp.Unix())
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	os.Stdout.Close()
	usr := make(chan os.Signal, 1)
	signal.Notify(usr, syscall.SIGUSR1)
	go func() {
		for range usr {
			offer()
		}
	}()
	fmt.Fprintln(os.Stderr, srv.Serve())
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
