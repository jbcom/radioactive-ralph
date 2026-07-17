package provider

// This file documents the agy (Antigravity CLI) local-surface spike, per
// spec §9 ("+ agy spike") and directive step 5. No runner is registered
// for agy — see the finding below.
//
// Spike method (2026-07-16, against the installed `agy` 1.1.3 CLI):
//
//  1. `agy --help` shows `-p, --print` ("Run a single prompt
//     non-interactively and print the response"), `--model`, and
//     `--project`/`--new-project` flags — on the surface this looks like
//     it could be a local one-shot agent CLI comparable to `claude -p` or
//     `codex exec`.
//  2. Running `agy --print "say hi"` (and again pinning
//     `--model "Gemini 3.5 Flash (Low)"`, and again with `--new-project`)
//     failed every time with the opaque `Error: Agent execution
//     terminated due to error.`
//  3. Re-running with `--log-file` captured to disk showed the CLI is
//     NOT a local inference call: it authenticates via a keyring-stored
//     GCP OAuth identity ("ChainedAuth: authenticated via keyring
//     (effective: gcp)"), opens a streamed "conversation" against
//     `https://cloudcode-pa.googleapis.com` (Google's Cloud Code /
//     Antigravity backend), and only then fails with
//     `agent executor error: invalid project ID: ""` — i.e. it requires a
//     resolvable CLOUD PROJECT before it will run a print-mode turn at
//     all. There is no local-only code path; the "print" mode still
//     round-trips through a cloud control surface that owns the
//     conversation object, project binding, and quota/experiment state.
//
// Conclusion: agy fails the local-only bar in spec §9 ("no cloud control
// surface in the loop"). It is classified Unknown (see
// internal/agentdetect.Detect, candidate "agy"), NOT Supported, and no
// OpencodeRunner-shaped AgyRunner or DefaultAgyProvider wiring is added to
// NewRunner/builtInProvider/shippedProviderBinaries. defaultAgyProvider in
// binding.go exists only to keep the capability-record shape and evidence
// documented next to its siblings — it is never reachable from
// ResolveBinding.
//
// agy's model catalog (`agy models`), documented here only because the
// directive asked for it: Gemini 3.5 Flash (Medium/High/Low), Gemini 3.1
// Pro (Low/High), Gemini 3 Flash.
