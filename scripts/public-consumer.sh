#!/usr/bin/env bash
# A fresh consumer, not a workspace build. Never re-use developer credentials/caches.
set -euo pipefail

usage() {
  echo "usage: bash scripts/public-consumer.sh local | published <exact-v0-or-v1-tag>" >&2
  exit 2
}

module=github.com/assurrussa/gonotify
mode=${1:-}
case "$mode" in
  local)
    [[ $# -eq 1 ]] || usage
    version=v0.0.0
    ;;
  published)
    [[ $# -eq 2 ]] || usage
    version=$2
    [[ $version =~ ^v[01]\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]] || usage
    ;;
  *) usage ;;
esac

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
go_bin=$(command -v go)
go_version=$(awk '$1 == "go" { print $2; exit }' "$repo/go.mod")
[[ -n $go_version ]] || { echo "missing go directive" >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/gonotify-public.XXXXXXXX")
success_message=

# env -i also drops GH_PAT, GITHUB_TOKEN, SSH_AUTH_SOCK, netrc/credential helpers,
# custom proxies, and ambient Go flags. Go can only use the public module proxy.
anonymous_go() {
  env -i PATH="$PATH" HOME="$work/home" XDG_CONFIG_HOME="$work/config" \
    TMPDIR="$work/tmp" GOPATH="$work/gopath" GOMODCACHE="$work/modules" \
    GOCACHE="$work/build" GOENV=off GOWORK=off GOFLAGS= GO111MODULE=on \
    GOTOOLCHAIN=local GOAUTH=off GOPRIVATE= GONOPROXY= GONOSUMDB= \
    GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org 'GOVCS=*:off' \
    GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null \
    GIT_CONFIG_COUNT=0 GIT_TERMINAL_PROMPT=0 \
    "$go_bin" "$@"
}

cleanup() {
  local probe_status=$?
  local cleanup_status=0
  trap - EXIT
  if [[ -d "$work/modules" ]]; then
    if anonymous_go clean -modcache; then
      :
    else
      cleanup_status=$?
    fi
  fi
  if [[ $cleanup_status -eq 0 ]]; then
    if rm -rf -- "$work"; then
      :
    else
      cleanup_status=$?
    fi
  fi
  if [[ $cleanup_status -ne 0 ]]; then
    echo "failed to clean temporary consumer: $work" >&2
  fi
  if [[ $probe_status -ne 0 ]]; then exit "$probe_status"; fi
  if [[ $cleanup_status -ne 0 ]]; then exit "$cleanup_status"; fi
  printf '%s\n' "$success_message"
}
trap cleanup EXIT
mkdir -p "$work/home" "$work/config" "$work/tmp" "$work/consumer"

# Re-use the maintained import manifest and public-API smoke test, not a second
# hand-maintained list. No published check imports gonotify implementation packages.
cp "$repo/reference/externalconsumer/imports.go" "$work/consumer/imports.go"
cp "$repo/reference/externalconsumer/consumer_test.go" "$work/consumer/consumer_test.go"
printf 'module example.com/gonotify-consumer\n\ngo %s\n\nrequire %s %s\n' \
  "$go_version" "$module" "$version" > "$work/consumer/go.mod"
cd "$work/consumer"
if [[ $mode == local ]]; then
  anonymous_go mod edit "-replace=$module=$repo"
fi

# Download the declared graph before tidy can prune unused requirements.
anonymous_go mod download all
anonymous_go mod tidy
anonymous_go mod verify
selected=$(anonymous_go list -m -f '{{.Path}} {{.Version}}' "$module")
[[ $selected == "$module $version" ]] || {
  echo "unexpected selected module: $selected; expected $module $version" >&2
  exit 1
}
replaced=$(anonymous_go list -m -f '{{if .Replace}}{{.Path}}{{end}}' all | sed '/^$/d')
expected_replaced=
if [[ $mode == local ]]; then expected_replaced=$module; fi
[[ $replaced == "$expected_replaced" ]] || {
  echo "unexpected module replacements: $replaced" >&2
  exit 1
}
anonymous_go test -mod=readonly -race -count=1 ./...
if [[ $mode == local ]]; then
  success_message="PASS: anonymous dependencies and local external consumer (not publication evidence)"
else
  success_message="PASS: anonymous external consumer for $module@$version"
fi
