#!/usr/bin/env bash
# Prints X.Y.Z[-PRE] when <commit> is a merged release commit: its subject is exactly
# "release: vX.Y.Z (#N)", as the squash merge of a release pull request writes it, and the VERSION
# file at that commit holds the same version. For any other commit it prints nothing. It fails
# when the subject names a version that VERSION doesn't hold.
#
# A release branch's own "release: vX.Y.Z" commit has no pull request number, so it never counts:
# only the squash-merged commit on master does. tag-release.yml uses this to decide what to tag,
# and release.yml to check a tag before building it.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <commit>" >&2
  exit 2
fi

subject="$(git log -1 --format=%s "$1")"

if [[ ! "${subject}" =~ ^release:\ v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?)\ \(#[0-9]+\)$ ]]; then
  exit 0
fi
version="${BASH_REMATCH[1]}"

got="$(git show "$1:VERSION" | tr -d '[:space:]')"
if [[ "${got}" != "${version}" ]]; then
  echo "VERSION is ${got}, but \"${subject}\" releases ${version}" >&2
  exit 1
fi

echo "${version}"
