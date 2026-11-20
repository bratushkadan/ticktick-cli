package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// APITimeLayout is TickTick's wire format "yyyy-MM-dd'T'HH:mm:ssZ"
// (offset without colon, e.g. 2019-11-13T03:00:00+0000). It is NOT RFC3339.
const APITimeLayout = "2006-01-02T15:04:05-0700"

var inputLayouts = []string{
	APITimeLayout,
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// NormalizeTime converts user input (RFC3339, "YYYY-MM-DD HH:MM",
// "YYYY-MM-DD", or already-API-format) into the TickTick wire format.
// Empty input returns "" (field omitted).
func NormalizeTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	for _, l := range inputLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format(APITimeLayout), nil
		}
	}
	return "", fmt.Errorf("invalid time %q: use YYYY-MM-DD, \"YYYY-MM-DD HH:MM\", or RFC3339", s)
}

// FmtTime renders an API timestamp for humans (local zone, minute precision).
// Unparseable values are returned unchanged.
func FmtTime(s string) string {
	if s == "" {
		return ""
	}
	for _, l := range []string{APITimeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, s); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
	}
	return s
}

// FmtEpochMs renders epoch milliseconds (used by OpenComment created/modified
// times in the live API) for humans; 0 → "".
func FmtEpochMs(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04")
}

// ParseStamp accepts YYYYMMDD ("20260407") or "2026-04-07" and returns
// the integer stamp used by habit check-ins and countdowns.
func ParseStamp(s string) (int32, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "-", ""))
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || len(s) != 8 {
		return 0, fmt.Errorf("invalid date stamp %q: use YYYYMMDD or YYYY-MM-DD", s)
	}
	return int32(n), nil
}

// StampToDate renders an integer YYYYMMDD stamp as "2006-01-02".
func StampToDate(stamp int32) string {
	s := strconv.FormatInt(int64(stamp), 10)
	if len(s) != 8 {
		return s
	}
	return s[:4] + "-" + s[4:6] + "-" + s[6:]
}
