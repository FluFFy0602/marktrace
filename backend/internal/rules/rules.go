// Package rules holds every business rule of MarkTrace (SPEC §1): GTIN, code mask,
// time format, lifecycle transitions. It is pure (no I/O) and shared by the import
// and form paths.
package rules

import (
	"regexp"
	"strings"
	"time"
)

// Version is the rules version stamped on attempts and violations.
const Version = "2026.11-1"

// CodeMask is the marking code mask (SPEC §1.2).
const CodeMask = `^01(\d{14})21([A-Za-z0-9]{6})$`

var codeRe = regexp.MustCompile(CodeMask)

// Code statuses in lifecycle order.
const (
	StatusIssued     = "issued"
	StatusIntroduced = "introduced"
	StatusSold       = "sold"
	StatusReturned   = "returned"
	StatusDisposed   = "disposed"
)

// Statuses lists all code statuses in lifecycle order.
var Statuses = []string{StatusIssued, StatusIntroduced, StatusSold, StatusReturned, StatusDisposed}

// Operation types.
const (
	OpIssue     = "issue"
	OpIntroduce = "introduce"
	OpSell      = "sell"
	OpReturn    = "return"
	OpDispose   = "dispose"
)

// OpTypes lists all operation types.
var OpTypes = []string{OpIssue, OpIntroduce, OpSell, OpReturn, OpDispose}

// Transition describes one lifecycle rule. From is empty for issue ("no accepted history").
type Transition struct {
	OpType string   `json:"op_type"`
	From   []string `json:"from"`
	To     string   `json:"to"`
}

// Transitions is the lifecycle table (SPEC §1.4).
var Transitions = []Transition{
	{OpIssue, []string{}, StatusIssued},
	{OpIntroduce, []string{StatusIssued}, StatusIntroduced},
	{OpSell, []string{StatusIntroduced, StatusReturned}, StatusSold},
	{OpReturn, []string{StatusSold}, StatusReturned},
	{OpDispose, []string{StatusIntroduced, StatusSold, StatusReturned}, StatusDisposed},
}

// IsStatus reports whether s is a valid CodeStatus.
func IsStatus(s string) bool { return contains(Statuses, s) }

// IsOpType reports whether s is a valid OperationType.
func IsOpType(s string) bool { return contains(OpTypes, s) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// NextStatus returns the status after applying opType to a code in status from.
// from == "" means the code has no accepted history. ok is false when the
// transition is not allowed.
func NextStatus(opType, from string) (string, bool) {
	for _, t := range Transitions {
		if t.OpType != opType {
			continue
		}
		if opType == OpIssue {
			if from == "" {
				return t.To, true
			}
			return "", false
		}
		if from != "" && contains(t.From, from) {
			return t.To, true
		}
		return "", false
	}
	return "", false
}

// CheckGTIN validates a GTIN-13/14 and returns "" or a reason code.
func CheckGTIN(g string) string {
	if len(g) != 13 && len(g) != 14 {
		return GTINFormat
	}
	for i := 0; i < len(g); i++ {
		if g[i] < '0' || g[i] > '9' {
			return GTINFormat
		}
	}
	if gtinCheckDigit(g[:len(g)-1]) != g[len(g)-1]-'0' {
		return GTINChecksum
	}
	return ""
}

// gtinCheckDigit computes the GS1 mod-10 check digit for the body (without check digit).
func gtinCheckDigit(body string) byte {
	sum := 0
	w := 3
	for i := len(body) - 1; i >= 0; i-- {
		sum += int(body[i]-'0') * w
		if w == 3 {
			w = 1
		} else {
			w = 3
		}
	}
	return byte((10 - sum%10) % 10)
}

// NormalizeGTIN returns the 14-digit comparison form. Only for comparison.
func NormalizeGTIN(g string) string {
	if len(g) == 13 {
		return "0" + g
	}
	return g
}

// ParseCode matches the code mask and returns the embedded GTIN-14.
func ParseCode(code string) (gtin string, ok bool) {
	m := codeRe.FindStringSubmatch(code)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ParseTime parses RFC 3339 with a mandatory offset and whole seconds.
func ParseTime(s string) (time.Time, bool) {
	// Layout requires seconds and an explicit offset or Z; fractions are rejected.
	if len(s) < 20 || s[10] != 'T' || strings.ContainsAny(s, ".,") {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	// time.RFC3339 tolerates lowercase t/z; keep the input strict.
	if strings.ContainsAny(s, "tz") {
		return time.Time{}, false
	}
	return t, true
}

// FormatTime renders a timestamp in canonical UTC output form.
func FormatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
