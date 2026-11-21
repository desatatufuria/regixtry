package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// RepositoryGrantRow is one grant joined to its owner, as the all-grants
// listing reads it. Repository is plain text because a grant can name a
// repository that was never pushed. It carries no secret material.
type RepositoryGrantRow struct {
	Repository string
	UserID     string
	Username   string
	IsRobot    bool
	Role       RepoRole
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// GrantCursor is the keyset position of the all-grants listing: the last row
// of the previous page. Rows are totally ordered by (Repository, Username,
// UserID), compared bytewise.
type GrantCursor struct {
	Repository string `json:"r"`
	Username   string `json:"u"`
	UserID     string `json:"i"`
}

const maxGrantCursorBytes = 1024

// AtOrBefore reports whether row sorts at or before the cursor position, so
// it was already served.
func (c GrantCursor) AtOrBefore(row RepositoryGrantRow) bool {
	return CompareGrantKeys(row.Repository, row.Username, row.UserID, c.Repository, c.Username, c.UserID) <= 0
}

// CompareGrantKeys is the total order of the all-grants listing.
func CompareGrantKeys(repoA, userA, idA, repoB, userB, idB string) int {
	if c := strings.Compare(repoA, repoB); c != 0 {
		return c
	}
	if c := strings.Compare(userA, userB); c != 0 {
		return c
	}
	return strings.Compare(idA, idB)
}

// CursorFor returns the cursor that resumes right after row.
func CursorFor(row RepositoryGrantRow) GrantCursor {
	return GrantCursor{Repository: row.Repository, Username: row.Username, UserID: row.UserID}
}

// Encode returns the opaque text form of the cursor (unpadded base64url JSON).
func (c GrantCursor) Encode() string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeGrantCursor parses an opaque cursor strictly: bounded size, canonical
// unpadded base64url, one JSON object with exactly the known non-empty fields,
// valid UTF-8 and no trailing data. Any failure is a validation error.
func DecodeGrantCursor(text string) (GrantCursor, error) {
	invalid := NewValidationError("after is not a valid cursor; use the next value of a previous response")
	if text == "" || len(text) > maxGrantCursorBytes {
		return GrantCursor{}, invalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil {
		return GrantCursor{}, invalid
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cursor GrantCursor
	if err := decoder.Decode(&cursor); err != nil {
		return GrantCursor{}, invalid
	}
	if decoder.More() {
		return GrantCursor{}, invalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return GrantCursor{}, invalid
	}
	if cursor.Repository == "" || cursor.Username == "" || cursor.UserID == "" ||
		hasControl(cursor.Repository) || hasControl(cursor.Username) || hasControl(cursor.UserID) {
		return GrantCursor{}, invalid
	}
	// Canonical form: the payload must be byte-for-byte what Encode emits, which
	// rejects duplicate keys, reordering, whitespace, invalid UTF-8 (decoding
	// rewrites it to U+FFFD) and non-canonical escapes.
	if cursor.Encode() != text {
		return GrantCursor{}, invalid
	}
	return cursor, nil
}

func hasControl(text string) bool {
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
