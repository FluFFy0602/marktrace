// Package reports serves read models: code status report, code history, violations,
// imports, KPI and golden comparison (SPEC §5–§7).
package reports

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrBadFilter is returned for unparseable filter values.
var ErrBadFilter = errors.New("bad filter")

// where accumulates SQL conditions with positional arguments.
type where struct {
	conds []string
	args  []any
}

func (w *where) add(cond string, v any) {
	w.args = append(w.args, v)
	w.conds = append(w.conds, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(w.args))))
}

func (w *where) addRaw(cond string) { w.conds = append(w.conds, cond) }

func (w *where) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

func (w *where) clone() *where {
	return &where{conds: append([]string(nil), w.conds...), args: append([]any(nil), w.args...)}
}

// next returns the placeholder for the next argument.
func (w *where) next(v any) string {
	w.args = append(w.args, v)
	return fmt.Sprintf("$%d", len(w.args))
}

// ParseBound parses a from/to filter value. Accepted: RFC 3339, "YYYY-MM-DDTHH:MM[:SS]"
// (treated as UTC) and "YYYY-MM-DD". For a date-only upper bound the whole day is included.
// Returns the bound and whether the comparison must be exclusive.
func ParseBound(s string, upper bool) (time.Time, bool, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, false, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if upper {
			return t.Add(24 * time.Hour), true, nil
		}
		return t, false, nil
	}
	return time.Time{}, false, fmt.Errorf("%w: дата «%s» не распознана", ErrBadFilter, s)
}

// addPeriod adds from/to conditions on a timestamp column.
func addPeriod(w *where, col, from, to string) error {
	if from != "" {
		t, _, err := ParseBound(from, false)
		if err != nil {
			return err
		}
		w.add(col+" >= ?", t)
	}
	if to != "" {
		t, excl, err := ParseBound(to, true)
		if err != nil {
			return err
		}
		if excl {
			w.add(col+" < ?", t)
		} else {
			w.add(col+" <= ?", t)
		}
	}
	return nil
}

// Paging parses limit/offset with defaults.
func Paging(limitS, offsetS string, def, max int) (int, int, error) {
	limit, offset := def, 0
	if limitS != "" {
		n, err := strconv.Atoi(limitS)
		if err != nil || n < 0 {
			return 0, 0, fmt.Errorf("%w: limit", ErrBadFilter)
		}
		limit = n
	}
	if limit > max {
		limit = max
	}
	if offsetS != "" {
		n, err := strconv.Atoi(offsetS)
		if err != nil || n < 0 {
			return 0, 0, fmt.Errorf("%w: offset", ErrBadFilter)
		}
		offset = n
	}
	return limit, offset, nil
}
