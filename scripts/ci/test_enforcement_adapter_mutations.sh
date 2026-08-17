#!/usr/bin/env bash
set -euo pipefail

# Each case re-applies one guarded adapter defect in an isolated source tree,
# proves that the mutation landed on the intended exact line, proves the
# mutated package still compiles, and then requires the named regression test
# to fail for its specific assertion. This is intentionally separate from the
# ordinary green suite: a passing test alone cannot prove it detects the bug.

root="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

baseline="$tmp/baseline"
mkdir -p "$baseline"
git -C "$root" archive --format=tar HEAD | tar -xf - -C "$baseline"

mutate_and_expect_failure() {
  local name="$1"
  local file="$2"
  local line="$3"
  local expected="$4"
  local replacement="$5"
  local package="$6"
  local test_name="$7"
  local failure_text="$8"
  local work="$tmp/$name"
  local path
  local actual

  cp -R "$baseline" "$work"
  path="$work/$file"
  actual="$(sed -n "${line}p" "$path")"
  if [[ "$actual" != "$expected" ]]; then
    printf 'mutation %s anchor drifted at %s:%s\nexpected: %s\nactual:   %s\n' \
      "$name" "$file" "$line" "$expected" "$actual" >&2
    return 1
  fi

  if ! (cd "$work" && go test -count=1 -run "^${test_name}$" "$package") \
    >"$work/baseline.log" 2>&1; then
    printf 'mutation %s baseline is already red; detection cannot be proven\n' \
      "$name" >&2
    sed -n '1,160p' "$work/baseline.log" >&2
    return 1
  fi

  awk -v target_line="$line" -v replacement="$replacement" \
    'NR == target_line { $0 = replacement } { print }' \
    "$path" >"$path.mutated"
  mv "$path.mutated" "$path"
  printf 'mutation %s landed: %s:%s: %s\n' \
    "$name" "$file" "$line" "$(sed -n "${line}p" "$path")"

  if ! (cd "$work" && go test -count=1 -run '^$' "$package") \
    >"$work/compile.log" 2>&1; then
    printf 'mutation %s did not compile; regression test never ran\n' "$name" >&2
    sed -n '1,160p' "$work/compile.log" >&2
    return 1
  fi

  if (cd "$work" && go test -count=1 -run "^${test_name}$" "$package") \
    >"$work/test.log" 2>&1; then
    printf 'mutation %s survived %s\n' "$name" "$test_name" >&2
    return 1
  fi
  if ! grep -Fq -- "$failure_text" "$work/test.log"; then
    printf 'mutation %s failed for an unexpected reason; wanted %q\n' \
      "$name" "$failure_text" >&2
    sed -n '1,160p' "$work/test.log" >&2
    return 1
  fi
  printf 'mutation %s rejected by %s (%s)\n' \
    "$name" "$test_name" "$failure_text"
}

mutate_and_expect_failure \
  hook-protocol-version internal/ipc/server.go 623 \
  $'\tif req.ProtoVersion < HookProtoVersion {' \
  $'\tif false {' \
  ./internal/ipc TestHookIPCIsVersionedStrictAndSecretBlind 'response ='

mutate_and_expect_failure \
  hook-strict-decoding internal/ipc/server.go 636 \
  $'\tdecoder.DisallowUnknownFields()' \
  $'\tdecoder.UseNumber()' \
  ./internal/ipc TestHookIPCIsVersionedStrictAndSecretBlind 'response ='

mutate_and_expect_failure \
  inherited-coordinate-stripping internal/provider/hook_environment.go 38 \
  $'\t\t\tcontinue' \
  $'\t\t\t// defect: retain inherited managed coordinates' \
  ./internal/provider TestManagedHookEnvironmentStripsInheritedCoordinatesFromUnmanagedTurn \
  'filtered environment ='

mutate_and_expect_failure \
  partial-coordinate-rejection internal/provider/hook_environment.go 24 \
  $'\tif (req.ManagedSessionID == "") != (req.HookEndpoint == "") {' \
  $'\tif false {' \
  ./internal/provider TestManagedHookEnvironmentRejectsPartialCoordinates \
  'partial coordinates:'

mutate_and_expect_failure \
  explicit-acceptance-gate internal/orch/orchestrator.go 651 \
  $'\t\tif task == nil || !hasMechanicalAcceptance(task.AcceptanceJSON) {' \
  $'\t\tif task == nil {' \
  ./internal/orch TestConfigureManagedHooksRequiresExplicitAcceptanceForEveryTask \
  'mixed fanout was partially hook-managed'

mutate_and_expect_failure \
  stop-owner-recheck internal/orch/verify.go 286 \
  $'\tif reportingSession == "" || task.ClaimedBySession != reportingSession {' \
  $'\tif reportingSession == "" {' \
  ./internal/orch TestCanStopAsRechecksAcceptanceWithoutCompletingTask \
  'stale CanStopAs'

mutate_and_expect_failure \
  stop-acceptance-recheck internal/orch/verify.go 310 \
  $'\tif !ok {' \
  $'\tif !ok && false {' \
  ./internal/orch TestCanStopAsRechecksAcceptanceWithoutCompletingTask \
  'CanStopAs = true, want false'

mutate_and_expect_failure \
  stop-live-verdict internal/supervisor/hooks.go 95 \
  $'\t\tallPassed = false' \
  $'\t\tallPassed = true' \
  ./internal/supervisor TestHandleHookEventStopUsesLiveBindingAndIndependentAcceptance \
  'first stop reply ='

mutate_and_expect_failure \
  stop-live-provider internal/supervisor/hooks.go 51 \
  $'\t\tif task.Provider != args.Adapter {' \
  $'\t\tif task.Provider != args.Adapter && false {' \
  ./internal/supervisor TestHandleHookEventStopUsesLiveBindingAndIndependentAcceptance \
  'mismatch reply ='

mutate_and_expect_failure \
  pending-run-deduplication internal/supervisor/hooks.go 101 \
  $'\t\tif !s.claimHookRun(key) {' \
  $'\t\tif false {' \
  ./internal/supervisor TestHandleHookEventDoesNotParkStopBehindAcceptance \
  'pending Stop ='

mutate_and_expect_failure \
  progress-invalidation internal/supervisor/hooks.go 60 \
  $'\t\tif err := s.store.InvalidateHookVerifications(ctx, args.SessionID); err != nil {' \
  $'\t\tif err := error(nil); err != nil {' \
  ./internal/supervisor TestHandleHookEventStopUsesLiveBindingAndIndependentAcceptance \
  'post-progress stop reply ='

printf 'all enforcement adapter mutation proofs passed\n'
