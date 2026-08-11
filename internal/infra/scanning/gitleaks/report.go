package gitleaks

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// reportEntry decodes one gitleaks JSON report entry. It intentionally
// declares ONLY RuleID/Description/File/StartLine/EndLine/Tags.
//
// This is the structural redaction control from design.md decision 10: a
// real gitleaks report entry also carries Secret, Match, Fingerprint, and
// Entropy fields, but this struct has no field capable of holding any of
// them, so encoding/json silently discards those keys on decode. The leak is
// unrepresentable in the type — not merely "not currently written anywhere".
// Do not add a field like RawMatch "for debugging": that would reintroduce
// exactly the leak this struct exists to prevent.
type reportEntry struct {
	RuleID      string   `json:"RuleID"`
	Description string   `json:"Description"`
	File        string   `json:"File"`
	StartLine   int      `json:"StartLine"`
	EndLine     int      `json:"EndLine"`
	Tags        []string `json:"Tags,omitempty"`
}

// decodeReport parses a gitleaks --report-format json report body. An empty
// or blank body and an empty JSON array both decode to zero findings — both
// are valid shapes for "no leaks found" and are not decode failures.
func decodeReport(body []byte) ([]reportEntry, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var entries []reportEntry
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, fmt.Errorf("decode gitleaks report: %w", err)
	}
	return entries, nil
}
