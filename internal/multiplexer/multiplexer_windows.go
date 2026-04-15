//go:build windows

package multiplexer

// SpawnDetached on Windows is not supported. Ralph's supervisor requires
// a POSIX environment (Unix sockets, SIGTERM semantics, setsid, etc.);
// Windows operators run Ralph via WSL2+Linuxbrew where the unix build
// path applies. The binary on native Windows exists only for the
// config-manipulation subcommands (`radioactive_ralph init`, `radioactive_ralph status` against
// a remote supervisor via socket, `radioactive_ralph doctor`) — anything that would
// spawn a supervisor surfaces this error cleanly.
func (d *Detacher) SpawnDetached(req SpawnRequest) (Spawned, error) {
	return Spawned{}, ErrUnsupported
}

// ErrUnsupported is returned on Windows when the caller asks for a
// supervisor spawn. Use `errors.Is(err, ErrUnsupported)` to detect.
var ErrUnsupported = errUnsupported{}

type errUnsupported struct{}

func (errUnsupported) Error() string {
	return "multiplexer: supervisor spawn not supported on Windows; run Ralph via WSL2"
}
