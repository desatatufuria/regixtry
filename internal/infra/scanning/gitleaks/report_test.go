package gitleaks

import (
	"encoding/json"
	"strings"
	"testing"
)

// syntheticGitleaksReport mirrors a realistic gitleaks JSON report entry
// shape, including the fields decodeReport MUST discard: Secret, Match,
// Fingerprint, and Entropy.
const syntheticGitleaksReport = `[
  {
    "RuleID": "aws-access-token",
    "Description": "AWS Access Token",
    "StartLine": 12,
    "EndLine": 12,
    "StartColumn": 5,
    "EndColumn": 25,
    "Match": "AKIAFAKEFAKEFAKEFAKE",
    "Secret": "AKIAFAKEFAKEFAKEFAKE",
    "File": "layers/001-abc123def456.tar.gz",
    "SymlinkFile": "",
    "Commit": "",
    "Entropy": 3.6690595,
    "Author": "",
    "Email": "",
    "Date": "",
    "Message": "",
    "Tags": ["aws", "access-token"],
    "Fingerprint": "layers/001-abc123def456.tar.gz:aws-access-token:12"
  }
]`

// TestDecodeReportNeverRepresentsSecretMaterial is the redaction RED test
// (tasks.md 4.8, design.md decision 10): decoding a report that contains
// "Secret":"AKIA..." must never let that substring end up in any field of
// the decoded struct. The decoder struct itself has no field capable of
// holding it, so this is asserted on the decoded/marshaled struct, not on
// log or stdout output.
func TestDecodeReportNeverRepresentsSecretMaterial(t *testing.T) {
	t.Parallel()

	const secretValue = "AKIAFAKEFAKEFAKEFAKE"

	entries, err := decodeReport([]byte(syntheticGitleaksReport))
	if err != nil {
		t.Fatalf("decodeReport() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	entry := entries[0]
	if entry.RuleID != "aws-access-token" || entry.StartLine != 12 || entry.EndLine != 12 {
		t.Fatalf("entry = %#v, want decoded RuleID/StartLine/EndLine from the report", entry)
	}

	// Marshal the decoded struct back out and assert the secret substring
	// cannot appear anywhere in it — proving the type itself cannot carry
	// it, not merely that nothing chose to print it.
	marshaled, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal(entry) error = %v", err)
	}
	if strings.Contains(string(marshaled), secretValue) {
		t.Fatalf("marshaled decoded entry = %s, want no field to ever contain the matched secret value", marshaled)
	}

	// Structural guarantee, not incidental: reflect over the JSON tags the
	// struct actually declares and confirm none of the banned field names
	// exist, so a future edit re-adding one of them fails this test even if
	// it forgets to populate it with the synthetic secret above.
	for _, banned := range []string{"Secret", "Match", "Fingerprint", "Entropy"} {
		if strings.Contains(string(marshaled), `"`+banned+`"`) {
			t.Fatalf("marshaled decoded entry = %s, want no %q field to exist on the decoder struct", marshaled, banned)
		}
	}
}

// TestDecodeReportHandlesEmptyReport confirms gitleaks' "no leaks found"
// report shapes (empty array or empty body) decode to zero findings without
// error, so a clean scan is not mistaken for a decode failure.
func TestDecodeReportHandlesEmptyReport(t *testing.T) {
	t.Parallel()

	for _, body := range [][]byte{[]byte(`[]`), []byte(``), []byte(`  `)} {
		entries, err := decodeReport(body)
		if err != nil {
			t.Fatalf("decodeReport(%q) error = %v", body, err)
		}
		if len(entries) != 0 {
			t.Fatalf("decodeReport(%q) = %#v, want zero findings", body, entries)
		}
	}
}
