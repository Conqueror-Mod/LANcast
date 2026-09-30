#!/usr/bin/env bash
# apt-install.sh PACKAGE... — install packages on a GitHub runner without
# hanging on a mirror that stops answering.
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
set -uo pipefail

opts=(-o Acquire::Retries=3 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30)

for attempt in 1 2 3; do
  if timeout 120 sudo apt-get "${opts[@]}" update &&
     timeout 240 sudo apt-get "${opts[@]}" install -y "$@"; then
    exit 0
  fi
  echo "apt attempt ${attempt} of 3 failed or timed out; retrying" >&2
  sleep 10
done

echo "could not install $* after 3 attempts" >&2
exit 1
