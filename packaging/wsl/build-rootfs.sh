#!/usr/bin/env bash
# Builds packaging/wsl/rootfs.tar.gz for gowsl.Distro.Register(). See
# ../../docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md.
#
# Follows Microsoft's documented method exactly (Build a Custom Linux
# Distribution for WSL, learn.microsoft.com/windows/wsl/build-custom-distro):
# docker export a container made from the image, then re-tar with
# --numeric-owner --absolute-names so ownership/paths survive outside the
# original container's user namespace, gzip'd (not another compressor --
# Microsoft specifically calls out gzip for older-WSL-version compatibility).
#
# The extract/scrub/retar step deliberately runs *inside a Linux container*,
# not on the host running this script. A Debian rootfs is full of Unix
# symlinks that are dangling until every entry in the tar has been
# extracted (e.g. /usr/lib/ssl/certs -> /etc/ssl/certs, created before
# /etc/ssl/certs exists yet). Windows' CreateSymbolicLink needs to know
# up front whether a symlink targets a file or a directory, which it can't
# determine for a not-yet-existing target -- so extracting this rootfs with
# Windows-native tar fails outright ("Cannot create symlink ... No such
# file or directory") partway through. Doing it inside a real Linux
# container sidesteps the problem instead of working around it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

IMAGE_TAG="radioactive-ralph-wsl-rootfs:build"
OUT="rootfs.tar.gz"

docker buildx build --target rootfs -t "$IMAGE_TAG" --load .

container_id="$(docker create "$IMAGE_TAG")"
trap 'docker rm -f "$container_id" >/dev/null 2>&1 || true' EXIT

# Docker Desktop's volume mounts need a real Windows-style path on Windows;
# a Git-Bash-style /c/... path silently resolves to nothing inside the
# container ("Directory nonexistent"). `pwd -W` (a Git-Bash-specific
# extension -- this script requires Git-Bash or WSL to run on Windows at
# all, see README.md) gives that Windows-style path there and falls back
# to plain `pwd` everywhere else (macOS/Linux, or WSL where the path is
# already correct as-is).
host_dir="$(pwd -W 2>/dev/null || pwd)"

docker export "$container_id" | docker run --rm -i \
    -v "$host_dir:/out" \
    debian:13-slim \
    sh -c '
        set -eu
        mkdir -p /tmp/rootfs
        tar -x -C /tmp/rootfs
        cd /tmp/rootfs

        # Per Microsoft'"'"'s guidance: no resolv.conf (WSL2 manages DNS
        # itself), and no password hashes in shadow.
        rm -f etc/resolv.conf
        if [ -f etc/shadow ]; then
            sed -i "s/^\([^:]*\):[^:]*:/\1:*:/" etc/shadow
        fi

        tar --numeric-owner --absolute-names -c * | gzip --best > /out/'"$OUT"'
    '

echo "Wrote $(pwd)/$OUT ($(du -h "$OUT" | cut -f1))"
