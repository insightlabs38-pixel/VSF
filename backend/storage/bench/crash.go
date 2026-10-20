package bench

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
)

// ChildWriter is the body of the process the crash test kills: it writes objects and reports each acknowledged one on stdout.
func ChildWriter(ctx context.Context, t Target, seed uint64, n, workers int) error {
	b, err := t.Open(ctx)
	if err != nil {
		return err
	}
	objs := Tiny(seed, n)
	var next atomic.Int64
	var wg sync.WaitGroup
	var out sync.Mutex
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(objs) {
					return
				}
				if err := b.Put(ctx, objs[i].Key, objs[i].Data); err != nil {
					return
				}
				out.Lock()
				fmt.Fprintf(os.Stdout, "A %d\n", i)
				out.Unlock()
			}
		}()
	}
	wg.Wait()
	return t.Close()
}

func verify(ctx context.Context, b blob.Backend, corpus []Object, acked map[int]bool) Crash {
	var c Crash
	c.Acked = len(acked)
	for i := range acked {
		got, err := b.Get(ctx, corpus[i].Key)
		switch {
		case errors.Is(err, blob.ErrNotFound):
			c.AckedLost++
		case err != nil:
			c.AckedLost++
		case !bytes.Equal(got, corpus[i].Data):
			c.AckedCorrupt++
		}
	}
	after := ""
	known := map[string]bool{}
	for i := range corpus {
		known[corpus[i].Key] = true
	}
	for {
		page, err := b.List(ctx, "pub/", after, 1000)
		if err != nil || len(page) == 0 {
			break
		}
		for _, o := range page {
			after = o.Key
			data, err := b.Get(ctx, o.Key)
			if err != nil {
				continue
			}
			if !verifyDigest(o.Key, data) {
				c.PresentTorn++
			}
		}
		if len(page) < 1000 {
			break
		}
	}
	return c
}

// CrashRunner for targets that live in this process: the writer is a child that is killed with SIGKILL.
func ChildCrash(exe string, runs int, seed uint64, base string, k int, factory func(dir string) Target, name string) CrashRunner {
	return func(ctx context.Context, _ Target, _ []Object) ([]Crash, error) {
		corpus := Tiny(seed, k)
		var out []Crash
		for run := 0; run < runs; run++ {
			dir := filepath.Join(base, fmt.Sprintf("crash-%d", run))
			killAfter := time.Duration(300+int(newRng(seed+uint64(run)).next()%900)) * time.Millisecond
			cmd := exec.Command(exe, "-child-writer", "-target", name, "-dir", dir, "-seed", strconv.FormatUint(seed, 10), "-objects", strconv.Itoa(k))
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				return out, err
			}
			if err := cmd.Start(); err != nil {
				return out, err
			}
			acked := map[int]bool{}
			done := make(chan struct{})
			go func() {
				sc := bufio.NewScanner(pipe)
				for sc.Scan() {
					if rest, ok := strings.CutPrefix(sc.Text(), "A "); ok {
						if i, err := strconv.Atoi(rest); err == nil {
							acked[i] = true
						}
					}
				}
				close(done)
			}()
			time.Sleep(killAfter)
			_ = cmd.Process.Signal(syscall.SIGKILL)
			_, _ = cmd.Process.Wait()
			<-done
			t := factory(dir)
			start := time.Now()
			b, err := t.Open(ctx)
			if err != nil {
				out = append(out, Crash{Run: run, Acked: len(acked), ReopenFailure: true, KillAfterMs: killAfter.Milliseconds()})
				continue
			}
			c := verify(ctx, b, corpus, acked)
			c.RecoveryMs = time.Since(start).Milliseconds()
			c.Run, c.KillAfterMs = run, killAfter.Milliseconds()
			_ = t.Close()
			out = append(out, c)
		}
		return out, nil
	}
}

// ServerCrash for a separate server process: the parent writes and acknowledges, then kills the server.
func ServerCrash(runs int, seed uint64, base string, k int, factory func(dir string) Target) CrashRunner {
	return func(ctx context.Context, _ Target, _ []Object) ([]Crash, error) {
		corpus := Tiny(seed, k)
		var out []Crash
		for run := 0; run < runs; run++ {
			t := factory(filepath.Join(base, fmt.Sprintf("crash-%d", run)))
			b, err := t.Open(ctx)
			if err != nil {
				return out, err
			}
			killAfter := time.Duration(300+int(newRng(seed+uint64(run)).next()%900)) * time.Millisecond
			acked := map[int]bool{}
			var mu sync.Mutex
			var next atomic.Int64
			var wg sync.WaitGroup
			stop := make(chan struct{})
			for w := 0; w < 4; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-stop:
							return
						default:
						}
						i := int(next.Add(1)) - 1
						if i >= len(corpus) {
							return
						}
						if err := b.Put(ctx, corpus[i].Key, corpus[i].Data); err != nil {
							return
						}
						mu.Lock()
						acked[i] = true
						mu.Unlock()
					}
				}()
			}
			time.Sleep(killAfter)
			_ = t.Kill()
			close(stop)
			wg.Wait()
			start := time.Now()
			b, err = t.Open(ctx)
			if err != nil {
				out = append(out, Crash{Run: run, Acked: len(acked), ReopenFailure: true, KillAfterMs: killAfter.Milliseconds()})
				continue
			}
			c := verify(ctx, b, corpus, acked)
			c.RecoveryMs = time.Since(start).Milliseconds()
			c.Run, c.KillAfterMs = run, killAfter.Milliseconds()
			_ = t.Close()
			out = append(out, c)
		}
		return out, nil
	}
}
