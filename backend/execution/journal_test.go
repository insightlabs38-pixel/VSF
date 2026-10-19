package execution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestATornFinalLineIsDroppedAndTheChainContinuesFromTheLastDurableEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := OpenJournal(NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.Create("op_1", 1, map[string]string{"operation_id": "x", "session": "s", "record": "r"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Move("op_1", Admitted, "", "", 2); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"seq":3,"key":"op_1","kind":"state","from":"admi`)
	_ = f.Close()

	j2, err := OpenJournal(NewFileStore(path))
	if err != nil {
		t.Fatalf("a torn tail bricked the journal: %v", err)
	}
	if rec, _ := j2.Get("op_1"); rec.State != Admitted || len(rec.Events) != 2 {
		t.Fatalf("recovered %+v", rec)
	}
	if err := j2.Move("op_1", Running, "", "{}", 3); err != nil {
		t.Fatal(err)
	}
	j3, err := OpenJournal(NewFileStore(path))
	if err != nil {
		t.Fatalf("the repaired file no longer replays: %v", err)
	}
	if rec, _ := j3.Get("op_1"); rec.State != Running || len(rec.Events) != 3 {
		t.Fatalf("after repair %+v", rec)
	}
}

func TestACorruptMiddleLineIsStillRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, _ := OpenJournal(NewFileStore(path))
	_, _, _ = j.Create("op_1", 1, nil)
	_ = j.Move("op_1", Admitted, "", "", 2)
	raw, _ := os.ReadFile(path)
	lines := strings.Split(string(raw), "\n")
	lines[0] = strings.Replace(lines[0], `"at":1`, `"at":9`, 1)
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
	if _, err := OpenJournal(NewFileStore(path)); err == nil {
		t.Fatal("a rewritten event was accepted")
	}
}

func TestAnOversizedSourceMessageCannotBrickTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, _ := OpenJournal(NewFileStore(path))
	_, _, _ = j.Create("op_1", 1, nil)
	_ = j.Move("op_1", Admitted, "", "", 2)
	_ = j.Move("op_1", Running, "", "{}", 3)
	if err := j.Move("op_1", Unknown, ReasonUnknownCommit, strings.Repeat("é", 2<<20), 4); err != nil {
		t.Fatal(err)
	}
	j2, err := OpenJournal(NewFileStore(path))
	if err != nil {
		t.Fatalf("an oversized detail made the journal unreadable: %v", err)
	}
	if rec, _ := j2.Get("op_1"); rec.State != Unknown || len(rec.Detail) > maxDetail+4 {
		t.Fatalf("detail %d bytes", len(rec.Detail))
	}
}

func TestStartRefusesAnOverlapInTheSameCriticalSectionAsTheUnknownTransition(t *testing.T) {
	j, _ := OpenJournal(&MemStore{})
	fp := Footprint{App: "a", Session: "s", Record: "r", Level: LevelRecord, Exclusive: true}
	create := func(key string) {
		meta := footprintMeta(fp)
		meta["session"], meta["record"] = "s", "r"
		if _, _, err := j.Create(key, 1, meta); err != nil {
			t.Fatal(err)
		}
		if err := j.Move(key, Admitted, "", "{}", 2); err != nil {
			t.Fatal(err)
		}
	}
	create("op_a")
	create("op_b")
	if err := j.Start("op_a", "{}", 3); err != nil {
		t.Fatal(err)
	}
	if err := j.Move("op_a", Unknown, ReasonUnknownCommit, "", 4); err != nil {
		t.Fatal(err)
	}
	if err := j.Start("op_b", "{}", 5); err == nil {
		t.Fatal("started over an unresolved overlap")
	}
	if err := j.Move("op_a", Failed, "reconciled_not_committed", "", 6); err != nil {
		t.Fatal(err)
	}
	if err := j.Start("op_b", "{}", 7); err != nil {
		t.Fatalf("the exclusion outlived the unresolved write: %v", err)
	}
}

func TestARecordWithoutAFootprintBlocksItsWholeSession(t *testing.T) {
	j, _ := OpenJournal(&MemStore{})
	_, _, _ = j.Create("op_old", 1, map[string]string{"session": "s", "record": "r1"})
	_ = j.Move("op_old", Admitted, "", "", 2)
	_ = j.Move("op_old", Running, "", "{}", 3)
	_ = j.Move("op_old", Unknown, ReasonUnknownCommit, "", 4)
	other := Footprint{App: "", Session: "s", Record: "r2", Level: LevelRecord}
	if _, blocked := j.Blocking(other, "op_new"); !blocked {
		t.Fatal("an older journal entry with no recorded scope must be treated as covering its session")
	}
}
