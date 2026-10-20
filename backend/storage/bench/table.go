package bench

import (
	"fmt"
	"strings"
)

// Table renders the headline numbers of several targets side by side.
func Table(reps []Report) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("%-34s", "")
	for _, r := range reps {
		w(" %16s", r.Target)
	}
	w("\n")
	row := func(label string, f func(Report) string) {
		w("%-34s", label)
		for _, r := range reps {
			w(" %16s", f(r))
		}
		w("\n")
	}
	phase := func(r Report, name string) *Phase {
		for i := range r.Phases {
			if strings.HasPrefix(r.Phases[i].Name, name) {
				return &r.Phases[i]
			}
		}
		return nil
	}
	for _, name := range []string{"write tiny", "read tiny", "write similar", "read large", "mixed"} {
		name := name
		row(name+" ops/s", func(r Report) string {
			if p := phase(r, name); p != nil {
				return fmt.Sprintf("%.0f", p.OpsPerSec)
			}
			return "-"
		})
		row(name+" p50/p95/p99 ms", func(r Report) string {
			if p := phase(r, name); p != nil {
				return fmt.Sprintf("%.2f/%.2f/%.2f", float64(p.Latency.P50us)/1000, float64(p.Latency.P95us)/1000, float64(p.Latency.P99us)/1000)
			}
			return "-"
		})
	}
	row("tiny: bytes at rest / logical", func(r Report) string {
		return fmt.Sprintf("%.2fx", float64(r.SizeTinyBytes)/float64(max(r.TinyLogical, 1)))
	})
	row("tiny: device writes / logical", func(r Report) string {
		if p := phase(r, "write tiny"); p != nil && r.TinyLogical > 0 {
			return fmt.Sprintf("%.2fx", float64(p.IO.WriteBytes)/float64(r.TinyLogical))
		}
		return "-"
	})
	row("versions: bytes at rest / logical", func(r Report) string {
		return fmt.Sprintf("%.2fx", float64(r.SizeVersions-r.SizeTinyBytes)/float64(max(r.VersionsLogical, 1)))
	})
	row("peak RSS above start MiB", func(r Report) string { return fmt.Sprintf("%d", (r.PeakRSSKiB-r.BaselineRSSKiB)/1024) })
	row("cold start ms", func(r Report) string { return fmt.Sprintf("%d", r.ColdStartMs) })
	row("delete half: s", func(r Report) string { return fmt.Sprintf("%.1f", r.Compaction.DeleteSeconds) })
	row("maintenance: s", func(r Report) string { return fmt.Sprintf("%.1f", r.Compaction.MaintSeconds) })
	row("bytes after delete -> maintain MiB", func(r Report) string {
		return fmt.Sprintf("%d>%d", r.Compaction.SizeAfterDel>>20, r.Compaction.SizeAfterMaint>>20)
	})
	row("crash: acked lost/corrupt/torn", func(r Report) string {
		var lost, corrupt, torn, acked int
		for _, c := range r.Crash {
			lost += c.AckedLost
			corrupt += c.AckedCorrupt
			torn += c.PresentTorn
			acked += c.Acked
		}
		if len(r.Crash) == 0 {
			return "-"
		}
		return fmt.Sprintf("%d/%d/%d of %d", lost, corrupt, torn, acked)
	})
	row("crash: recovery ms (max)", func(r Report) string {
		var m int64
		for _, c := range r.Crash {
			m = max(m, c.RecoveryMs)
		}
		return fmt.Sprintf("%d", m)
	})
	row("errors", func(r Report) string {
		n := 0
		for _, p := range r.Phases {
			n += p.Errors
		}
		return fmt.Sprintf("%d", n)
	})
	return b.String()
}
