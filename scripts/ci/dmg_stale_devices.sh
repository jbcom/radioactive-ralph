#!/usr/bin/env bash
# Parse `hdiutil info` output and print the /dev/diskN devices backed by a
# given disk-image path.
#
# This lives in its own file so the parser can be TESTED. It was previously
# inline in finalize_gui_bundle.sh, where the only way to exercise it was to
# run a macOS packaging job that starts with no stale device attached -- so a
# parser that silently emitted nothing looked exactly like a parser that
# correctly found nothing to detach.
#
# Reads `hdiutil info` output on stdin so the test can feed a fixture.

# Block layout, verified against real `hdiutil info` output: each image starts
# at a ==== separator, `image-path` comes BEFORE its /dev/diskN entries, and
# the device field is /dev/disk5 rather than a bare "/dev/disk".
dmg_stale_devices() {
  local img="$1"
  awk -v img="$img" '
    /^=====/                           { mine = 0; next }
    /^image-path *:/                   { mine = (index($0, img) > 0); next }
    mine && $1 ~ /^\/dev\/disk[0-9]+$/ { print $1 }
  '
}
