#!/usr/bin/env bash
# Install a verified Linux relay binary without modifying a reverse proxy.
set -euo pipefail

if [[ $# != 3 ]]; then
  echo "Usage: $0 BINARY SHA256 REVISION" >&2
  exit 2
fi
binary=$1
expected=$2
revision=$3
if [[ ! $expected =~ ^[0-9a-f]{64}$ || ! $revision =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$ ]]; then
  echo "Invalid SHA256 or revision" >&2
  exit 2
fi
if [[ $(id -u) != 0 || $(uname -s) != Linux ]]; then
  echo "Run as root on Linux" >&2
  exit 2
fi
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
unit_source="$script_dir/../deploy/homing-relay.service"
if [[ ! -f $binary || ! -f $unit_source ]]; then
  echo "Binary or packaged service unit missing" >&2
  exit 2
fi
base=/opt/homing-relay
unit=/etc/systemd/system/homing-relay.service
lock=/run/lock/homing-relay-install.lock
exec 9>"$lock"
flock -n 9 || { echo "Another relay installation is in progress" >&2; exit 1; }

# Inspect the unit systemd actually loads, including runtime/vendor units and
# aliases. A matching file in /etc alone does not establish service ownership.
fragment=$(systemctl show homing-relay.service --property=FragmentPath --value)
dropins=$(systemctl show homing-relay.service --property=DropInPaths --value)
load_state=$(systemctl show homing-relay.service --property=LoadState --value)
unit_state=$(systemctl show homing-relay.service --property=UnitFileState --value)
names=$(systemctl show homing-relay.service --property=Names --value)
if [[ -n $dropins || $load_state == masked || $unit_state == masked* ||
      ( -n $fragment && $fragment != "$unit" ) || -L $unit ]]; then
  echo "Refusing an existing foreign, masked or overridden service" >&2
  exit 1
fi
for name in $names; do
  if [[ $name != homing-relay.service ]]; then
    echo "Refusing a service with aliases" >&2
    exit 1
  fi
done
had_unit=false
if [[ -e $unit ]]; then
  if [[ ! -f $unit ]] || ! cmp -s "$unit_source" "$unit"; then
    echo "Existing service unit differs; review it before replacing" >&2
    exit 1
  fi
  had_unit=true
fi
previous=
if [[ -e "$base/current" && ! -L "$base/current" ]]; then
  echo "Refusing to replace an unmanaged current directory" >&2
  exit 1
fi
if [[ -L "$base/current" ]]; then
  previous=$(readlink -f "$base/current")
  case "$previous" in "$base"/releases/*) ;; *) echo "Unmanaged current target" >&2; exit 1 ;; esac
  if [[ ! -x "$previous/homing-server" ]]; then
    echo "Previous release is not runnable; review it before replacing" >&2
    exit 1
  fi
fi
was_active=false
if systemctl is-active --quiet homing-relay.service; then was_active=true; fi
was_enabled=false
if systemctl is-enabled --quiet homing-relay.service >/dev/null 2>&1; then was_enabled=true; fi
if [[ $was_active == true && ( $had_unit != true || -z $previous ) ]]; then
  echo "Refusing an active service without a managed release" >&2
  exit 1
fi

release="$base/releases/$revision"
install -d -m 0755 "$release"
staged=$(mktemp "$release/.homing-server.XXXXXX")
mutated=false
success=false
finish() {
  code=$?
  trap - EXIT INT TERM
  set +e
  rm -f -- "$staged" "$base/current.new.$$" "$base/rollback.$$"
  if [[ $success != true && $mutated == true ]]; then
    echo "Relay failed validation; restoring previous service state" >&2
    systemctl stop homing-relay.service
    if [[ $was_enabled != true ]]; then systemctl disable homing-relay.service; fi
    if [[ -n $previous ]]; then
      ln -s "$previous" "$base/rollback.$$" && mv -Tf "$base/rollback.$$" "$base/current"
    else
      rm -f -- "$base/current"
    fi
    if [[ $had_unit != true ]]; then rm -f -- "$unit"; fi
    systemctl daemon-reload
    if [[ $was_active == true ]]; then systemctl restart homing-relay.service; fi
  fi
  exit "$code"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Verify the root-owned staged bytes, not the upload path, then atomically
# publish. A failed/interrupted copy never reserves a broken release revision.
install -m 0755 "$binary" "$staged"
actual=$(sha256sum -- "$staged")
if [[ ${actual%% *} != "$expected" ]]; then
  echo "Binary checksum mismatch; service unchanged" >&2
  exit 1
fi
if [[ -e "$release/homing-server" || -L "$release/homing-server" ]]; then
  installed=$(sha256sum "$release/homing-server")
  if [[ -L "$release/homing-server" || ${installed%% *} != "$expected" ]]; then
    echo "This revision already names a different binary" >&2
    exit 1
  fi
else
  mv -Tf "$staged" "$release/homing-server"
fi
mutated=true
if [[ $had_unit != true ]]; then install -m 0644 "$unit_source" "$unit"; fi
ln -s "$release" "$base/current.new.$$"
mv -Tf "$base/current.new.$$" "$base/current"
systemctl daemon-reload
systemctl restart homing-relay.service
healthy=false
for attempt in {1..15}; do
  if systemctl is-active --quiet homing-relay.service &&
      curl --noproxy '*' -fsS --max-time 2 http://127.0.0.1:18789/health | \
      python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(not (d.get("ok") is True and d.get("service")=="homing-server"))'; then
    healthy=true
    break
  fi
  sleep 1
done
if [[ $healthy != true ]]; then exit 1; fi
systemctl enable homing-relay.service
success=true
printf 'Installed Homing relay revision %s; SHA256 %s\n' "$revision" "$expected"
printf 'Backend: http://127.0.0.1:18789 (loopback only)\n'
