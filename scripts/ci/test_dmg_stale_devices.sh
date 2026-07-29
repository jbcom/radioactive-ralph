#!/usr/bin/env bash
# Regression test for the hdiutil stale-device parser.
#
# WHY THIS EXISTS: the packaging jobs start with no stale DMG attached, so a
# parser that silently emits nothing is indistinguishable from one that
# correctly finds nothing to detach. The first version of this parser WAS a
# silent no-op -- it looked for `image-path` AFTER the /dev/diskN lines and
# matched a bare "/dev/disk" that never appears -- and a green packaging job
# would not have caught it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=scripts/ci/dmg_stale_devices.sh
source ./dmg_stale_devices.sh

# A fixture in the real shape of `hdiutil info`, including a SECOND image so a
# parser that ignores block boundaries and prints every device it sees fails.
fixture() {
  cat <<'EOF'
framework       : 683.100.3
images          : 2
================================================
image-path      : /Users/runner/work/other_1.2.3_darwin_amd64.dmg
image-type      : read-only disk image
/dev/disk9	GUID_partition_scheme	
/dev/disk9s1	41504653-0000-11AA-AA11-00306543ECAC	/Volumes/other
================================================
image-path      : /Users/runner/work/radioactive-ralph_1.2.3_darwin_amd64.dmg
image-type      : read-only disk image
/dev/disk5	GUID_partition_scheme	
/dev/disk5s1	41504653-0000-11AA-AA11-00306543ECAC	/Volumes/radioactive-ralph
/dev/disk6	EF57347C-0000-11AA-AA11-00306543ECAC	
EOF
}

target=/Users/runner/work/radioactive-ralph_1.2.3_darwin_amd64.dmg
fail=0

got="$(fixture | dmg_stale_devices "$target" | tr '\n' ' ')"
want="/dev/disk5 /dev/disk6 "
if [[ "$got" != "$want" ]]; then
  echo "FAIL: devices for the target image: got [$got] want [$want]" >&2
  fail=1
fi

# The other image's device must NOT be returned: detaching a device that
# belongs to an unrelated disk image is worse than the bug being fixed.
if [[ "$got" == *disk9* ]]; then
  echo "FAIL: returned /dev/disk9, which belongs to a different image" >&2
  fail=1
fi

# A path that is not attached must yield nothing -- the case that made the
# original no-op parser look like it was working.
if [[ -n "$(fixture | dmg_stale_devices /Users/runner/work/absent.dmg)" ]]; then
  echo "FAIL: returned devices for an image that is not attached" >&2
  fail=1
fi

# Empty input (no images attached at all) must be handled without error.
if [[ -n "$(printf '' | dmg_stale_devices "$target")" ]]; then
  echo "FAIL: returned devices for empty hdiutil output" >&2
  fail=1
fi

if (( fail )); then
  echo "dmg_stale_devices: FAILED" >&2
  exit 1
fi
echo "dmg_stale_devices: ok"
