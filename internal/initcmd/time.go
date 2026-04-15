package initcmd

import "time"

// nowUTC is a seam so tests can pin the date in scaffolded frontmatter.
var nowUTC = func() time.Time { return time.Now().UTC() }

// todayISO returns today's date in YYYY-MM-DD, used in the plans
// scaffolding frontmatter. Pinning nowUTC in tests keeps the
// generated file byte-identical across runs.
var todayISO = defaultToday

func defaultToday() string {
	return nowUTC().Format("2006-01-02")
}
