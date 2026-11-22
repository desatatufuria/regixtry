package regixtry

import (
	"strings"
	"testing"
)

func TestValidateRuntimeVersion(t *testing.T) {
	t.Parallel()

	valid := []string{"1.2.3", "v1.2.3", "0.73.0", "v0.58.0", "1.2.3-rc.1", "1.2.3-beta", "10.20.30-alpha-1.x"}
	for _, version := range valid {
		if err := ValidateRuntimeVersion(version); err != nil {
			t.Errorf("ValidateRuntimeVersion(%q) error = %v, want nil", version, err)
		}
	}

	invalid := map[string]string{
		"empty":             "",
		"two parts":         "1.2",
		"four parts":        "1.2.3.4",
		"leading zero":      "01.2.3",
		"word":              "latest",
		"slash":             "1.2.3/x",
		"traversal":         "1.2.3/../../x",
		"dotdot":            "..",
		"encoded slash":     "1.2.3%2fx",
		"encoded dotdot":    "%2e%2e",
		"query":             "1.2.3?a=b",
		"fragment":          "1.2.3#x",
		"scheme":            "https://evil.example/1.2.3",
		"authority":         "//evil.example/1.2.3",
		"backslash":         `1.2.3\x`,
		"space":             "1.2.3 ",
		"inner space":       "1.2. 3",
		"newline":           "1.2.3\n",
		"nul":               "1.2.3\x00",
		"tab":               "1.2.\t3",
		"empty prerelease":  "1.2.3-",
		"dotdot prerelease": "1.2.3-a..b",
		"unicode":           "1.2.3-é",
		"huge":              "1.2." + strings.Repeat("3", 100000),
		"just over limit":   "1.2." + strings.Repeat("3", MaxRuntimeVersionLength),
	}
	for name, version := range invalid {
		if err := ValidateRuntimeVersion(version); err == nil {
			t.Errorf("%s: ValidateRuntimeVersion(%.40q) error = nil, want a validation error", name, version)
		} else if !IsCode(err, ErrorCodeValidation) {
			t.Errorf("%s: error code = %v, want validation", name, err)
		}
	}
}
