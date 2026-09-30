#!/usr/bin/env bash
# apt-install.sh PACKAGE... — install packages on a GitHub runner without
# hanging on a mirror that stops answering, and without failing on one that is
# merely slow.
#
# Why this exists: the v0.9.43 and v0.9.44 release builds both stuck on a plain
# `apt-get update && apt-get install` for 14 and 4+ minutes, against a normal
# 25 seconds. Nothing failed; a mirror simply stopped answering, and apt waited.
# A job's default limit is six hours, so the release sat with no installer
# until somebody noticed and restarted it — and a re-run worked immediately.
#
# So: each apt call gets a hard time limit and apt's own network retries and
# per-connection timeout, and the whole thing is tried three times. A hung
# mirror now costs a few minutes and a retry, not the release.
#
# The install's limit is sized from what is left to download, not fixed. The
# first version gave it a flat 240 seconds, and on 30 September the Faces
# worker failed three times in a row against a mirror serving package files at
# ~250 KB/s — a normal run fetches the same 115 MB at 90 MB/s. The mirror never
# stopped; every attempt was making progress. Finished .debs survive a retry
# (the second and third attempts needed 85 MB and then 36 MB), but the file in
# flight does not, and the last one was 35 MB: at under 150 KB/s it could not
# finish inside 240 seconds however many times it was tried. A fixed limit
# turned "slow" into "impossible". Now the limit allows every remaining byte at
# FLOOR_BPS plus a fixed allowance for dpkg, so a slow mirror finishes and a
# stalled one is still cut off — silent connections by apt's own 30-second
# timeout, trickles by the budget.
#
# APT_ARCHIVES, if set, is a directory to keep downloaded .debs in, so a CI
# cache can restore them and the download leaves the critical path entirely
# (.github/actions/apt-install). apt verifies every .deb against the signed
# indexes whether it came from the mirror or from the cache, so a stale or
# damaged cache costs a download, never a wrong package.
set -uo pipefail

FLOOR_BPS=131072 # 128 KB/s — about a thousandth of a healthy mirror
SLACK=120        # seconds for dpkg, and for the first connection

opts=(-o Acquire::Retries=3 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30)

if [ -n "${APT_ARCHIVES:-}" ]; then
  sudo mkdir -p "${APT_ARCHIVES}/partial"
  # apt downloads as _apt and falls back to root, noisily, if it cannot write.
  sudo chown _apt "${APT_ARCHIVES}/partial" 2>/dev/null || true
  opts+=(-o "Dir::Cache::Archives=${APT_ARCHIVES}/")
fi

for attempt in 1 2 3; do
  if timeout 120 sudo apt-get "${opts[@]}" update; then
    # What is still to fetch: --print-uris omits anything already in the
    # archive directory, so a retry's budget covers only what the last one
    # did not finish. Field 3 is the size in bytes.
    need="$(sudo apt-get "${opts[@]}" install --print-uris -qq "$@" | awk '{ s += $3 } END { print s + 0 }')"
    budget=$(( SLACK + need / FLOOR_BPS ))
    echo "apt attempt ${attempt}: ${need} bytes to fetch, allowing ${budget}s"
    if timeout "${budget}" sudo apt-get "${opts[@]}" install -y "$@"; then
      # Drop .debs no longer in the indexes, so a cache that is restored and
      # saved again week after week does not keep every version it ever held.
      if [ -n "${APT_ARCHIVES:-}" ]; then
        sudo apt-get "${opts[@]}" autoclean -qq || true
      fi
      exit 0
    fi
  fi
  echo "apt attempt ${attempt} of 3 failed or timed out; retrying" >&2
  sleep 10
done

echo "could not install $* after 3 attempts" >&2
exit 1
