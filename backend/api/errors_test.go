package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEveryCodeHasOneFixedMeaning(t *testing.T) {
	exitFor := map[Category]map[int]bool{}
	for _, c := range Codes() {
		i, ok := taxonomy[c]
		if !ok || i.Summary == "" || i.HTTP < 400 || i.HTTP > 599 {
			t.Fatalf("%s has no usable info: %+v", c, i)
		}
		if i.Exit == ExitOK {
			t.Fatalf("%s reuses the success exit code", c)
		}
		if (i.Effect == EffectUnknown) != (i.Category == CategoryUnknownCommit || i.Category == CategoryUnverifiedEffect || c == CodeReconcileInProgress) {
			t.Fatalf("%s: effect %s does not fit category %s", c, i.Effect, i.Category)
		}
		if i.Effect == EffectUnknown && (i.Retry == RetryFixRequest || i.Retry == RetryNewRequestID) {
			t.Fatalf("%s invites a blind retry of an ambiguous write: %s", c, i.Retry)
		}
		if exitFor[i.Category] == nil {
			exitFor[i.Category] = map[int]bool{}
		}
		exitFor[i.Category][i.Exit] = true
	}
	for _, c := range []Code{CodeUnknownCommit, CodeUnverifiedEffect} {
		if InfoOf(c).Retry != RetryReconcile {
			t.Fatalf("%s must route to reconciliation", c)
		}
	}
	if InfoOf("no_such_code").Code() != CodeInternal {
		t.Fatal("an unknown code must read as internal")
	}
}

func (i Info) Code() Code {
	for c, x := range taxonomy {
		if x == i {
			return c
		}
	}
	return ""
}

func TestUnknownOutcomesNeverShareAnExitOrStatusWithPlainFailures(t *testing.T) {
	for _, c := range []Code{CodeUnknownCommit, CodeUnverifiedEffect, CodeStillUnknown} {
		i := InfoOf(c)
		for _, other := range Codes() {
			o := InfoOf(other)
			if o.Category == CategoryFailed && o.Exit == i.Exit {
				t.Fatalf("%s shares exit %d with failed code %s", c, i.Exit, other)
			}
		}
	}
	if InfoOf(CodeUnknownCommit).Exit == InfoOf(CodeUnverifiedEffect).Exit {
		t.Fatal("unknown commit and unverified effect must be distinguishable by exit code")
	}
}

// Every reason the runtime can record must reach a client as a specific code, not as an internal error.
func TestEveryRuntimeReasonIsClassified(t *testing.T) {
	reasons := map[string]bool{}
	lit := regexp.MustCompile(`(?:refuse|finishWith\([^,]+,[^,]+,)\(?\s*"([a-z_]+)(?::|")`)
	files, _ := filepath.Glob(filepath.Join("..", "execution", "*.go"))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range lit.FindAllStringSubmatch(string(b), -1) {
			reasons[m[1]] = true
		}
		for _, m := range regexp.MustCompile(`Failed, "([a-z_]+)"`).FindAllStringSubmatch(string(b), -1) {
			reasons[m[1]] = true
		}
	}
	for _, r := range []string{"rejected", "conflict", "throttled", "unavailable", "stale_read", "stale_observation", "seen_required", "read_failed"} {
		reasons[r] = true
	}
	internalOK := map[string]bool{"undecodable": true, "not_operation_contract": true, "digest_disagreement": true,
		"identity_unrecorded": true, "contract_mismatch": true, "unknown_session": true, "other_source": true,
		"read": true, "verified": true, "acknowledged": true, "reconciled_committed": true}
	if len(reasons) < 40 {
		t.Fatalf("found only %d reasons; the scan is not seeing the runtime", len(reasons))
	}
	for r := range reasons {
		if ClassifyReason(r) == CodeInternal && !internalOK[r] {
			t.Errorf("reason %q reaches clients as internal", r)
		}
	}
}

func TestErrorsNeverCarryCallerText(t *testing.T) {
	e := errWith(CodeInvalidArgument, "schema", map[string]any{"violations": []any{}})
	if e.Message != InfoOf(CodeInvalidArgument).Summary {
		t.Fatal("message must be the fixed text of the code")
	}
	if AsError(os.ErrNotExist).Code != CodeInternal || strings.Contains(AsError(os.ErrNotExist).Error(), "file") {
		t.Fatal("a foreign error must be hidden behind internal")
	}
}
