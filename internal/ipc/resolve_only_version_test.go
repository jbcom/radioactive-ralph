package ipc

import "testing"

// TestResolveOnlyRequiresANewerProtocolVersion is CodeRabbit's P2 on #246.
//
// ResolveOnly CHANGES the meaning of an existing command: ProjectEnsure
// normally accumulates and touches the project, and this flag promises it will
// not. An older supervisor does not know the field, so it silently ignores it
// and runs the mutating path — which is precisely what the flag promises does
// not happen. Version skew here is routine: the desktop binary is upgraded
// while a long-lived supervisor keeps running the previous build.
//
// Sending it at a version an old supervisor REJECTS turns a silent broken
// promise into a clear refusal.
func TestResolveOnlyRequiresANewerProtocolVersion(t *testing.T) {
	if ResolveOnlyProtoVersion <= DriveProtoVersion {
		t.Fatalf("ResolveOnlyProtoVersion = %d, must exceed DriveProtoVersion = %d — "+
			"otherwise a supervisor that predates the flag accepts the request and "+
			"silently runs the mutating path the flag promises to avoid",
			ResolveOnlyProtoVersion, DriveProtoVersion)
	}
	// It must exceed the PREVIOUS maximum (3), because a supervisor built before
	// this change refuses any req.ProtoVersion above its own — that refusal is
	// the whole mechanism. Written as a literal on purpose: comparing against
	// ProtoVersion would silently pass forever once this build bumps its own
	// maximum to match.
	const previousMaxProtoVersion = 3
	if ResolveOnlyProtoVersion <= previousMaxProtoVersion {
		t.Fatalf("ResolveOnlyProtoVersion = %d, must exceed the previous max (v%d) "+
			"so a supervisor predating the flag rejects the request instead of "+
			"ignoring the field and running the mutating path",
			ResolveOnlyProtoVersion, previousMaxProtoVersion)
	}
	// This build must also SPEAK it, or its own supervisor rejects its own client.
	if ProtoVersion < ResolveOnlyProtoVersion {
		t.Fatalf("ProtoVersion = %d < ResolveOnlyProtoVersion = %d; this build "+
			"would reject its own client", ProtoVersion, ResolveOnlyProtoVersion)
	}
}

// TestProjectEnsureWithoutResolveOnlyStaysOnTheDriveVersion is the
// compatibility guard. Only the CHANGED semantic needs the newer floor;
// bumping every ProjectEnsure call would break rolling upgrades against a
// supervisor that handles the plain command perfectly well.
func TestProjectEnsureWithoutResolveOnlyStaysOnTheDriveVersion(t *testing.T) {
	if got := projectEnsureProtoVersion(ProjectEnsureArgs{DisplayName: "x"}); got != DriveProtoVersion {
		t.Fatalf("plain ProjectEnsure sent at v%d, want v%d — a rolling client "+
			"must still drive an otherwise-compatible older supervisor",
			got, DriveProtoVersion)
	}
	if got := projectEnsureProtoVersion(ProjectEnsureArgs{DisplayName: "x", ResolveOnly: true}); got != ResolveOnlyProtoVersion {
		t.Fatalf("ResolveOnly request sent at v%d, want v%d", got, ResolveOnlyProtoVersion)
	}
}
