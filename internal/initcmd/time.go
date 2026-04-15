package initcmd

import "time"

// nowUTC is a seam so tests can pin the date in scaffolded frontmatter.
var nowUTC = func() time.Time { return time.Now().UTC() }
