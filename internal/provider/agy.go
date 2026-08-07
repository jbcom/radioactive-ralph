package provider

// This file documents the agy (Antigravity CLI) provider surface and why
// it is local-only, correcting the initial spike's misclassification.
//
// Spike (2026-07-16, against agy 1.1.3):
//
//  1. `agy --help` shows `-p, --print` ("Run a single prompt
//     non-interactively and print the response"), `--model`, `--effort`,
//     and `--output-format` (text, json, stream-json) — a local one-shot
//     agent CLI comparable to `claude -p` or `codex exec`.
//  2. `agy --print "say hi"` failed with `Error: Agent execution
//     terminated due to error.` The spike attributed this to a "cloud
//     control surface" because the CLI contacts
//     `cloudcode-pa.googleapis.com`.
//
// CORRECTION (2026-08-06, against agy 1.1.10):
//
// The spike's conclusion was WRONG. `cloudcode-pa.googleapis.com` is where
// Gemini inference is served — exactly analogous to `api.anthropic.com`
// for claude, `api.openai.com` for codex, and `127.0.0.1:11434` for ollama.
// The spec §9 "local-only" test is: where does the AGENT LOOP, TOOL
// EXECUTION, and FILE I/O run? For agy, all three are local — the CLI
// process reads files, runs commands, and writes changes on this machine.
// That is the same architecture as claude/codex/opencode: a local agent
// process that calls a hosted model for inference.
//
// The actual failure the spike saw was `neither PlanModel nor
// RequestedModel specified` — the CLI needs `--model` to be a value from
// its model catalog, and the catalog requires auth to resolve. The
// "invalid project ID" error was a downstream consequence of the same
// auth gap, not a fundamental architectural limitation.
//
// What agy actually needs to work under Ralph:
//
//  1. A one-time browser OAuth login (like claude's first-run flow).
//  2. The user's login shell environment inherited by the supervisor
//     (HOME, keychain access, PATH) — provided by `--inherit-shell-env`.
//  3. WritePaths declaration for ~/.gemini/antigravity-cli (conversation
//     state, cache, logs) — same as opencode declares ~/.local/share/opencode.
//  4. `--dangerously-skip-permissions` so Ralph's non-interactive PTY
//     doesn't block on tool-use approval prompts.
//  5. A model name from `agy models` once auth is established.
//
// No runner is registered yet — the stream-json framing differs from
// opencode's and needs a dedicated AgyRunner. The classification is
// Supported so doctor and detection report it correctly; wiring the runner
// is future work.
