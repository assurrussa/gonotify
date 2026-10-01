#!/usr/bin/env bash
# Offline tests of the probe's isolation and failure handling, NOT a Go build.
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
work=$(mktemp -d "${TMPDIR:-/tmp}/gonotify-probe-test.XXXXXXXX")
trap 'chmod -R u+w "$work"; rm -rf -- "$work"' EXIT
mkdir -p "$work/bin" "$work/repo with spaces/scripts" "$work/repo with spaces/reference/externalconsumer"
cp "$repo/scripts/public-consumer.sh" "$work/repo with spaces/scripts/"
cp "$repo/reference/externalconsumer/"{imports.go,consumer_test.go} \
  "$work/repo with spaces/reference/externalconsumer/"
printf 'module github.com/assurrussa/gonotify\n\ngo 1.27.0\n' > "$work/repo with spaces/go.mod"
cat > "$work/bin/go" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
bin=$(cd -- "$(dirname -- "$0")" && pwd -P)
[[ ! ${GH_PAT+x} && ! ${GITHUB_TOKEN+x} && ! ${SSH_AUTH_SOCK+x} ]]
[[ $GOENV == off && $GOWORK == off && $GOFLAGS == '' && $GOAUTH == off ]]
[[ $GOPRIVATE == '' && $GONOPROXY == '' && $GONOSUMDB == '' ]]
[[ $GOPROXY == https://proxy.golang.org && $GOSUMDB == sum.golang.org && $GOVCS == '*:off' ]]
[[ $GOTOOLCHAIN == local && $GIT_CONFIG_GLOBAL == /dev/null && $GIT_CONFIG_NOSYSTEM == 1 ]]
[[ $HOME == */gonotify-public.*/home && $GOMODCACHE == */gonotify-public.*/modules ]]
[[ ! -f "$HOME/.netrc" && ! -f "$HOME/.gitconfig" ]]
printf '%s\n' "$*" >> "$bin/commands"
printf '%s\n' "${HOME%/home}" > "$bin/probe-work"
case "$1 $2" in
  'mod edit') echo 'replace github.com/assurrussa/gonotify => /source' >> go.mod ;;
  'mod download')
    [[ ! -e "$GOMODCACHE/seen" ]]
    mkdir -p "$GOMODCACHE"
    touch "$GOMODCACHE/seen"
    mkdir -p "$GOMODCACHE/example.com/fixture@v1.0.0"
    echo fixture > "$GOMODCACHE/example.com/fixture@v1.0.0/go.mod"
    chmod -R a-w "$GOMODCACHE/example.com/fixture@v1.0.0"
    if [[ -f "$bin/fail-download" ]]; then exit 21; fi
    ;;
  'mod tidy'|'mod verify') ;;
  'list -m')
    if [[ ${*: -1} == all ]]; then
      if grep -q '^replace ' go.mod; then echo github.com/assurrussa/gonotify; fi
      if [[ -f "$bin/extra-replace" ]]; then echo example.com/unexpected; fi
    elif [[ -f "$bin/wrong-version" ]]; then
      echo 'github.com/assurrussa/gonotify v0.0.999'
    else
      awk '$1 == "require" {print $2 " " $3}' go.mod
    fi
    ;;
  'test -mod=readonly')
    [[ -f imports.go && -f consumer_test.go ]]
    if [[ -f "$bin/fail-test" ]]; then exit 22; fi
    ;;
  'clean -modcache')
    [[ -d "$GOMODCACHE/example.com/fixture@v1.0.0" ]]
    if [[ -f "$bin/fail-clean" ]]; then exit 23; fi
    chmod -R u+w "$GOMODCACHE"
    rm -rf -- "$GOMODCACHE"
    ;;
  *) echo "unexpected fake Go command: $*" >&2; exit 1 ;;
esac
FAKE
chmod +x "$work/bin/go"
probe() {
  PATH="$work/bin:$PATH" GH_PAT=sentinel GITHUB_TOKEN=sentinel SSH_AUTH_SOCK=/host/agent \
    GOPRIVATE=github.com/assurrussa GOPROXY=https://invalid.example GOAUTH=netrc \
    GOFLAGS=-mod=vendor GOENV=/invalid/config GOMODCACHE=/invalid/cache \
    TMPDIR="$work" \
    bash "$work/repo with spaces/scripts/public-consumer.sh" "$@"
}
expect_success() {
  probe "$@" > "$work/output" 2>&1
  grep -q '^PASS:' "$work/output"
  [[ $(tail -n 1 "$work/bin/commands") == 'clean -modcache' ]]
  [[ ! -e $(cat "$work/bin/probe-work") ]]
}
expect_failure() {
  local expected_status=$1
  local expect_retained=$2
  local probe_status
  shift 2
  if probe "$@" > "$work/output" 2>&1; then
    probe_status=0
  else
    probe_status=$?
  fi
  [[ $probe_status -eq $expected_status ]] || {
    echo "probe exit $probe_status; expected $expected_status" >&2
    cat "$work/output" >&2
    exit 1
  }
  if grep -q '^PASS:' "$work/output"; then
    echo 'failed probe printed PASS' >&2; exit 1
  fi
  local probe_work
  probe_work=$(cat "$work/bin/probe-work")
  if [[ $expect_retained == yes ]]; then
    [[ -d $probe_work ]]
    grep -q 'failed to clean temporary consumer:' "$work/output"
    chmod -R u+w "$probe_work"
    rm -rf -- "$probe_work"
  else
    [[ ! -e $probe_work ]]
  fi
}
expect_success local
expect_success published v0.4.1
before=$(wc -l < "$work/bin/commands")
for version in latest master v0.4.1/../../etc ''; do
  if probe published "$version" > /dev/null 2>&1; then
    echo "accepted non-tag version: $version" >&2; exit 1
  fi
done
[[ $(wc -l < "$work/bin/commands") -eq $before ]]
for failure in wrong-version extra-replace; do
  touch "$work/bin/$failure"
  expect_failure 1 no published v0.4.1
  rm "$work/bin/$failure"
done
touch "$work/bin/fail-download"
expect_failure 21 no local
rm "$work/bin/fail-download"
touch "$work/bin/fail-test"
expect_failure 22 no published v0.4.1
touch "$work/bin/fail-clean"
expect_failure 22 yes published v0.4.1
rm "$work/bin/fail-test"
expect_failure 23 yes local
rm "$work/bin/fail-clean"
echo 'PASS: offline probe isolation, version/replacement guards, read-only cache cleanup and failure propagation'
