#!/usr/bin/env bash
#
# Put the released libmpv-2.dll where the installer and the release archive
# expect it, and refuse anything that is not the file we built (ADR 0069).
#
#   third_party/libmpv/fetch.sh
#
# The DLL is not in git — it is tens of megabytes of decoder — so the release
# job fetches the one built by build.sh and published as a release asset. The
# hash is the whole point: a download nobody checks is a supply chain, and this
# file ships inside an installer users run.
#
# LIBMPV_URL and LIBMPV_SHA256 live in artifact.env beside this script, and are
# updated in the same commit that records a new build in PROVENANCE.md.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=artifact.env
source "$here/artifact.env"

out="$here/out/libmpv-2.dll"
mkdir -p "$here/out"

if [ -f "$out" ]; then
	have="$(sha256sum "$out" | cut -d' ' -f1)"
	if [ "$have" = "$LIBMPV_SHA256" ]; then
		echo "libmpv-2.dll already present and matches the recorded hash"
		exit 0
	fi
	echo "libmpv-2.dll is present but hashes $have; replacing it" >&2
fi

echo "fetching $LIBMPV_URL"
curl -fsSL --retry 3 -o "$out.tmp" "$LIBMPV_URL"

have="$(sha256sum "$out.tmp" | cut -d' ' -f1)"
if [ "$have" != "$LIBMPV_SHA256" ]; then
	rm -f "$out.tmp"
	echo "refusing it: sha256 $have, expected $LIBMPV_SHA256" >&2
	exit 1
fi
mv "$out.tmp" "$out"
echo "libmpv-2.dll verified against the recorded hash"
