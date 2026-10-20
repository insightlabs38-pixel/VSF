package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestADelegationNamesOnePersonAndOneCallerExpiresAndCanBeWithdrawn(t *testing.T) {
	cl := &clock{t: epoch}
	_, _, buf := newBroker(t)
	d := NewDelegations(cl.now, NewAudit(buf))
	if err := d.Allow("ada", "ada", epoch.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	for name, until := range map[string]time.Time{"already over": epoch, "beyond thirty days": epoch.Add(31 * 24 * time.Hour)} {
		if err := d.Allow("ada", "bob", until); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := d.Allow("ada", "bad id", epoch.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if err := d.Allow("ada", "bob", epoch.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !d.Allows("ada", "bob") || d.Allows("bob", "ada") || d.Allows("ada", "carol") || d.Allows("carol", "bob") {
		t.Fatal("a delegation runs one way between exactly two callers")
	}
	cl.t = epoch.Add(time.Hour)
	if d.Allows("ada", "bob") {
		t.Fatal("a delegation ends at its time")
	}
	cl.t = epoch
	_ = d.Allow("ada", "bob", epoch.Add(time.Hour))
	d.Withdraw("ada", "bob")
	d.Withdraw("ada", "bob")
	if d.Allows("ada", "bob") {
		t.Fatal("withdrawn")
	}
	log := buf.String()
	if strings.Count(log, `"event":"delegation_withdrawn"`) != 1 || !strings.Contains(log, `"reason":"self_delegation"`) {
		t.Fatalf("%s", log)
	}
	if n, err := VerifyAudit(strings.NewReader(log)); err != nil || n != 4 {
		t.Fatalf("%d %v", n, err)
	}
}
