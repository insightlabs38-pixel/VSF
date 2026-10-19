package execution

import "github.com/insightlabs38-pixel/vsf/backend/ir"

type Level string

const (
	LevelRecord  Level = "record"
	LevelSession Level = "session"
	LevelGlobal  Level = "global"
)

// Footprint is the part of a source one write may change, compared field by field so identifiers can never collide.
type Footprint struct {
	App       string
	Session   string
	Record    string
	Level     Level
	Exclusive bool
}

// Overlaps is true when changes under a and b could touch the same source state.
// Sessions are the unit of isolation; only the global scope reaches across them.
func (a Footprint) Overlaps(b Footprint) bool {
	if a.App != b.App {
		return false
	}
	if a.Level == LevelGlobal || b.Level == LevelGlobal {
		return true
	}
	if a.Session != b.Session {
		return false
	}
	if a.Level == LevelSession || b.Level == LevelSession {
		return true
	}
	return a.Record == b.Record
}

// footprintOf maps a write's declared concurrency scope onto what it excludes and what an unresolved outcome blocks.
// A write declaring no scope is exclusive nowhere but still blocks and is blocked by unresolved writes on its record.
func footprintOf(c ir.OperationContract, session, record string) (Footprint, error) {
	fp := Footprint{App: string(c.Source.AppID), Session: session, Record: record}
	switch c.Effect.Concurrency {
	case ir.ConcurrencyScopeNone:
		fp.Level = LevelRecord
		if record == "" {
			fp.Level = LevelSession
		}
		if c.Freshness.BeforeWrite == ir.FreshnessRequirementNone {
			return fp, refuse("write_scope_insufficient")
		}
	case ir.ConcurrencyScopeSourceSession:
		fp.Level, fp.Exclusive = LevelSession, true
	case ir.ConcurrencyScopeSourceSessionAndRecord:
		if record == "" {
			return fp, refuse("record_required")
		}
		fp.Level, fp.Exclusive = LevelRecord, true
	case ir.ConcurrencyScopeGlobalSource:
		fp.Level, fp.Exclusive = LevelGlobal, true
	default:
		return fp, refuse("concurrency_unknown")
	}
	return fp, nil
}
