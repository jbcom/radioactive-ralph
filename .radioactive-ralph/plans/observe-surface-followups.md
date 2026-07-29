# Verify the observe surface end to end

1. build every target including the tagged GUI

   ```ralph-task
   {"id": "build"}
   ```

2. run the full suite with the race detector

   ```ralph-task
   {"id": "race", "after": ["build"]}
   ```

3. run the live end-to-end dispatch under a real pty

   ```ralph-task
   {"id": "e2e", "after": ["build"]}
   ```

4. run the repo claim verifier

   ```ralph-task
   {"id": "claims", "after": ["build"]}
   ```

The provenance and partition projections landed across five surfaces
(store, observe, TUI, GUI, CLI). This group re-establishes that they agree,
which is the property most likely to rot: each renders the same markers
through shared helpers, and a change to one can silently diverge the others.

# Close the marker-parity gap

- add a parity test that enumerates the marker set once and asserts each
  renderer emits it, so a new surface cannot ship a silent subset

  ```ralph-task
  {"id": "parity", "after": ["race", "e2e", "claims"]}
  ```

Today parity is enforced by shared helpers plus per-renderer tests, not by
anything that fails when a fourth renderer appears and forgets a marker.
