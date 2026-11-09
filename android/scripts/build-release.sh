#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

export HOMING_RELEASE_STORE_FILE="${HOMING_RELEASE_STORE_FILE:-$HOME/.homing-keys/homing-release.jks}"
export HOMING_RELEASE_KEY_ALIAS="${HOMING_RELEASE_KEY_ALIAS:-homing}"

if [[ -z "${HOMING_RELEASE_STORE_PASSWORD:-}" || -z "${HOMING_RELEASE_KEY_PASSWORD:-}" ]]; then
    PASSWORD="$(security find-generic-password \
        -s homing-android-release \
        -a homing \
        -w)"
    export HOMING_RELEASE_STORE_PASSWORD="${HOMING_RELEASE_STORE_PASSWORD:-$PASSWORD}"
    export HOMING_RELEASE_KEY_PASSWORD="${HOMING_RELEASE_KEY_PASSWORD:-$PASSWORD}"
    unset PASSWORD
fi

exec ./gradlew :app:homingRelease "$@"
