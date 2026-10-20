package bench

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("VSF_BENCH_CHILD") == "1" {
		fs := flag.NewFlagSet("child", flag.ExitOnError)
		dir, name := fs.String("dir", "", ""), fs.String("target", "", "")
		seed, n := fs.Uint64("seed", 0, ""), fs.Int("objects", 0, "")
		_ = fs.Parse(childArgs())
		var t Target
		if *name == "sqlite" {
			t = NewSQLiteTarget(*dir)
		} else {
			t = NewFSTarget(*dir)
		}
		if err := ChildWriter(context.Background(), t, *seed, *n, 4); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func childArgs() []string {
	var out []string
	for i, a := range os.Args {
		if a == "-child-writer" {
			out = append(out, os.Args[i+1:]...)
		}
	}
	return out
}

func TestTheCorpusIsDeterministicAndContentAddressed(t *testing.T) {
	a, b := Tiny(7, 500), Tiny(7, 500)
	for i := range a {
		if a[i].Key != b[i].Key || !bytes.Equal(a[i].Data, b[i].Data) {
			t.Fatalf("object %d differs between runs", i)
		}
		if !verifyDigest(a[i].Key, a[i].Data) {
			t.Fatalf("%s is not the digest of its bytes", a[i].Key)
		}
		if n := len(a[i].Data); n < 100 || n >= 2000 {
			t.Fatalf("size %d", n)
		}
	}
	if Tiny(8, 5)[0].Key == a[0].Key {
		t.Fatal("another seed must give another corpus")
	}
	if p := Tiny(7, 50); p[49].Key != a[49].Key {
		t.Fatal("a shorter corpus must be a prefix of a longer one")
	}
	seen := map[string]bool{}
	for _, o := range a {
		if seen[o.Key] {
			t.Fatal("duplicate key")
		}
		seen[o.Key] = true
	}
}

func TestVersionsDifferByAboutTheRequestedShare(t *testing.T) {
	v := Versions(3, 6, 200000, 2)
	for i := 1; i < len(v); i++ {
		diff := 0
		for k := range v[i].Data {
			if v[i].Data[k] != v[i-1].Data[k] {
				diff++
			}
		}
		pct := float64(diff) * 100 / float64(len(v[i].Data))
		if pct < 1 || pct > 2.5 {
			t.Fatalf("version %d differs in %.2f%% of its bytes", i, pct)
		}
	}
}

func TestChunksCoverTheDataExactlyAndStayBounded(t *testing.T) {
	data := Versions(1, 1, 1<<20, 0)[0].Data
	var got []byte
	for _, c := range Chunks(data, 8192) {
		if len(c) > 8192*4 {
			t.Fatalf("chunk of %d", len(c))
		}
		got = append(got, c...)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("chunks must reassemble the data")
	}
}

func TestChunkDedupFindsSharedBytesAndOnlyThose(t *testing.T) {
	same := []Object{{Data: Versions(2, 1, 300000, 0)[0].Data}}
	same = append(same, Object{Data: append([]byte(nil), same[0].Data...)})
	l, u := DedupBytes(same, 8192)
	if l != 2*u {
		t.Fatalf("two equal objects hold one copy: %d %d", l, u)
	}
	v := Versions(4, 8, 400000, 2)
	vl, vu := DedupBytes(v, 8192)
	if vu >= vl || float64(vu)/float64(vl) > 0.6 {
		t.Fatalf("similar versions must dedup: %d of %d", vu, vl)
	}
	d := []Object{{Data: Versions(5, 1, 300000, 0)[0].Data}, {Data: Versions(6, 1, 300000, 0)[0].Data}}
	dl, du := DedupBytes(d, 8192)
	if dl != du {
		t.Fatalf("unrelated random objects share nothing: %d %d", dl, du)
	}
}

func TestSummarizeReportsTheRightPercentiles(t *testing.T) {
	var d []time.Duration
	for i := 1; i <= 100; i++ {
		d = append(d, time.Duration(i)*time.Millisecond)
	}
	s := Summarize(d)
	if s.N != 100 || s.P50us != 51000 || s.P95us != 96000 || s.P99us != 100000 || s.Maxus != 100000 || s.MeanUs != 50500 {
		t.Fatalf("%+v", s)
	}
	if Summarize(nil).N != 0 {
		t.Fatal("empty input")
	}
}

func TestAWholeRunMeasuresEveryWorkloadAndFindsNoLossAfterKills(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSF_BENCH_CHILD", "1")
	cfg := Config{Seed: 3, Tiny: 600, Versions: 6, VersionSize: 64 << 10, ChangePct: 2, ReadSample: 300, Workers: 4, MixedFor: 300 * time.Millisecond, DeleteShare: 0.5}
	for name, mk := range map[string]func(string) Target{"fs": func(d string) Target { return NewFSTarget(d) }, "sqlite": func(d string) Target { return NewSQLiteTarget(d) }} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			crash := ChildCrash(exe, 2, cfg.Seed, filepath.Join(base, "crash"), 4000, mk, name)
			rep, err := Run(context.Background(), mk(filepath.Join(base, "main")), cfg, crash)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Phases) != 5 {
				t.Fatalf("%d phases", len(rep.Phases))
			}
			for _, p := range rep.Phases {
				if p.Errors != 0 || p.Objects == 0 || p.Latency.N == 0 {
					t.Fatalf("%+v", p)
				}
			}
			if rep.SizeTinyBytes < rep.TinyLogical {
				t.Fatalf("bytes at rest %d are below the logical %d", rep.SizeTinyBytes, rep.TinyLogical)
			}
			if rep.Compaction.Deleted != 300 || rep.Compaction.SizeAfterMaint >= rep.Compaction.SizeBefore {
				t.Fatalf("%+v", rep.Compaction)
			}
			if len(rep.Crash) != 2 {
				t.Fatalf("%d crash runs", len(rep.Crash))
			}
			for _, c := range rep.Crash {
				if c.Acked == 0 || c.AckedLost != 0 || c.AckedCorrupt != 0 || c.PresentTorn != 0 || c.ReopenFailure {
					t.Fatalf("%+v", c)
				}
			}
			if rep.Dedup.SavedFraction <= 0 {
				t.Fatalf("%+v", rep.Dedup)
			}
			if rep.Target == "" {
				t.Fatal("unnamed target")
			}
		})
	}
}
