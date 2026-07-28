//go:build !darwin && !linux

package contain

// wrapCommand FAILS CLOSED on every platform without a verified primitive.
//
// Linux has candidates (Landlock on 5.13+, bubblewrap, user namespaces) and
// Windows has others (job objects with a restricted token, AppContainer), but
// none of them is wired up or TESTED here — and an untested containment claim
// is worse than an honest refusal, because callers would rely on it. Each
// platform lands with its own behavioral proof that a write outside the root
// is actually refused, the way darwin's did, or it does not land.
func wrapCommand(_ Policy, _ string, _ []string) (string, []string, error) {
	return "", nil, ErrContainmentUnavailable
}

func available() bool { return false }
