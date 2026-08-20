# Builds the rootfs for the "radioactive-ralph" WSL2 distro bundled with the
# Windows release. This image is never run as a container in production --
# it exists only so `docker export` can produce a rootfs tarball for
# `gowsl.Distro.Register()`. See
# ../../docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md
# for the full architecture and build-rootfs.sh in this directory for the
# export step.
#
# NOT true "distroless": Google's distroless images are deliberately
# shell-less, package-manager-less, and init-system-less (single static
# binary only) -- genuinely incompatible with this rootfs's two hard
# requirements, systemd as PID 1 and dispatching arbitrary provider-CLI shell
# commands via `wsl.exe -d ralph -- <cmd>`. (base-debian12, the variant most
# guides reference, is also past its 2026-08-09 deprecation; debian13 is
# current.) This instead applies distroless-style hygiene through a real
# multi-stage build: a builder stage does all apt work, and only the
# resulting minimized filesystem -- not apt/dpkg, docs, man pages, or apt
# lists -- survives into the final stage.
#
# Provider CLIs (claude/codex/opencode) are deliberately NOT installed here.
# Whether they run via WSL interop against the already-authenticated
# Windows-side install, or via a Linux-side install added later, is an open
# question in the design doc pending empirical validation -- don't bake in a
# guess either way.

FROM debian:13-slim AS builder

# Pinned against debian:13-slim's current candidates (checked 2026-08-20 via
# `docker run --rm debian:13-slim apt-cache policy <pkg>`) for reproducible
# builds. Debian stable eventually rotates old exact-version packages out of
# the mirror in favor of newer point releases, so a future rebuild failing to
# resolve one of these pins is expected maintenance, not a bug -- re-run the
# same apt-cache policy check and bump.
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        systemd=257.13-1~deb13u1 \
        ca-certificates=20250419 \
        git=1:2.47.3-0+deb13u1 \
        sudo=1.9.16p2-3+deb13u2 \
    && \
    # /etc/resolv.conf is NOT removed here: Docker bind-mounts it into the
    # build container for DNS resolution, so `rm` fails mid-build ("Device or
    # resource busy") -- it can only be removed once, after `docker export`,
    # when it's a plain file again. build-rootfs.sh does that. Per
    # Microsoft's WSL custom-distro guidance the rootfs must not ship its own
    # resolv.conf (WSL2 manages DNS itself) or a kernel/initramfs (WSL2
    # supplies the kernel).
    # Mask the systemd units Microsoft documents as known to conflict with
    # WSL2's own networking/tmpfs handling.
    systemctl mask systemd-resolved.service \
        systemd-networkd.service \
        NetworkManager.service \
        systemd-tmpfiles-setup.service \
        systemd-tmpfiles-clean.service \
        systemd-tmpfiles-clean.timer \
        systemd-tmpfiles-setup-dev-early.service \
        systemd-tmpfiles-setup-dev.service \
        tmp.mount && \
    # Distroless-style hygiene, stopping short of removing apt/dpkg
    # themselves: apt is a Debian "Essential: yes" package (removing it
    # requires --allow-remove-essential and drags out libapt-pkg/dpkg's own
    # dependents), and keeping it preserves the option to install a provider
    # CLI inside the distro later without a full rebuild -- see the "not yet
    # decided" note in the design spec. Cache/lists/docs are pure waste
    # either way and always safe to drop.
    rm -rf /var/cache/apt/archives/*.deb /var/lib/apt/lists/* \
        /usr/share/doc /usr/share/man /usr/share/locale \
        /var/log/*

# --- final stage: only the minimized filesystem, no build tooling ---
FROM scratch AS rootfs
# Intentional: this stage's whole point is to produce a full (minimized) OS
# rootfs for `wsl --import`/gowsl.Register, not a single app binary's runtime
# deps, so copying the entire builder filesystem is the actual goal here,
# not the anti-pattern DL3067 normally warns against.
# hadolint ignore=DL3067
COPY --from=builder / /
COPY wsl.conf /etc/wsl.conf

CMD ["/bin/sh"]
