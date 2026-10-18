package evidence

import (
	"testing"

	"github.com/google/uuid"
)

func TestComputeChecksumStable(t *testing.T) {
	b := Bundle{RunID: uuid.New(), AMLRuleVersion: "abc"}
	s1, err := ComputeChecksum(b)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}
	s2, err := ComputeChecksum(b)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}
	if s1 != s2 {
		t.Fatalf("checksum not stable: %s != %s", s1, s2)
	}
	if len(s1) != 64 {
		t.Fatalf("expected 64-char hex checksum, got %d", len(s1))
	}
}

func TestComputeChecksumIgnoresExistingChecksum(t *testing.T) {
	b := Bundle{RunID: uuid.New()}
	s1, _ := ComputeChecksum(b)

	b.Checksum = "deadbeefdeadbeef"
	s2, _ := ComputeChecksum(b)

	if s1 != s2 {
		t.Fatalf("checksum must ignore the pre-existing Checksum field: %s != %s", s1, s2)
	}
}

func TestVerifyChecksumDetectsTampering(t *testing.T) {
	b := Bundle{RunID: uuid.New(), AMLRuleVersion: "v1"}
	sum, _ := ComputeChecksum(b)
	b.Checksum = sum

	if ok, err := VerifyChecksum(b); err != nil || !ok {
		t.Fatalf("valid bundle should verify: ok=%v err=%v", ok, err)
	}

	b.AMLRuleVersion = "tampered"
	if ok, _ := VerifyChecksum(b); ok {
		t.Fatal("tampered bundle must fail verification")
	}
}
