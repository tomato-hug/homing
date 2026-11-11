#!/usr/bin/env bash
# Build console/ and copy its dist/ output into web/console/, where
# internal/api serves it from go:embed. Run after changing console/.
set -eu
cd "$(dirname "$0")/.."

(cd console && npm run build)

rm -rf web/console/assets
mkdir -p web/console
cp console/dist/index.html console/dist/favicon.svg web/console/
cp -R console/dist/assets web/console/

echo "控制台已更新：web/console/"
