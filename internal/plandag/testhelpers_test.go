package plandag

import "time"

// mustParseTime is a test helper for pinning clockwork's FakeClock.
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
