package operationevidence

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvidenceUnknownExitIsNull(t *testing.T) {
	e := Evidence{SchemaVersion: 1, EventID: "e", OperationID: "o", SessionID: 1, Sequence: 1, Actor: "student", Kind: "command", Status: "unknown"}
	if err := Validate(e); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(e)
	if !strings.Contains(string(raw), `"exit_code":null`) {
		t.Fatal(string(raw))
	}
}
func TestActorCannotBePromoted(t *testing.T) {
	if Validate(Evidence{Actor: "browser", Status: "succeeded"}) == nil {
		t.Fatal("browser admitted")
	}
}
func TestCaptureRedactsCompleteRecord(t *testing.T) {
	v := CaptureText("o", "stdout", []byte("Authorization: Bearer hidden-secret\npassword=secret-pass\n--token other-secret\nlast tail"), 1<<20)
	if strings.Contains(v.Content, "hidden-secret") || strings.Contains(v.Content, "secret-pass") || strings.Contains(v.Content, "other-secret") || !strings.Contains(v.Content, "last tail") {
		t.Fatal(v.Content)
	}
}
func TestCaptureLimitCannotLeakPartialSecret(t *testing.T) {
	v := CaptureText("o", "stdout", []byte(strings.Repeat("x", 100)+" token=secret-secret"), 110)
	if strings.Contains(v.Content, "secret") || v.Coverage.Output != "partial" {
		t.Fatal(v)
	}
	b := CaptureText("o", "stdout", []byte{0, 255}, 10)
	if b.Coverage.Output != "unavailable" {
		t.Fatal(b)
	}
}
func TestOwnedTextUTF8Tail(t *testing.T) {
	v := CaptureText("o", "stdout", []byte(strings.Repeat("中", 4000)+"TAIL"), 1<<20)
	var all string
	offset := 0
	for {
		f, err := ReadFragment(v, offset, 8192)
		if err != nil {
			t.Fatal(err)
		}
		all += f.Text
		offset = f.NextOffsetBytes
		if f.Complete {
			break
		}
	}
	if all != v.Content || !strings.HasSuffix(all, "TAIL") {
		t.Fatal("tail")
	}
	if _, err := ReadFragment(v, 0, 8193); err == nil {
		t.Fatal("oversized fragment")
	}
}
