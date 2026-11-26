#!/usr/bin/env bash
# Build console/ and copy its dist/ output into web/console/, where
# internal/api serves it from go:embed. Run after changing console/.
set -eu
cd "$(dirname "$0")/.."

if [ ! -d console/node_modules ]; then
  (cd console && npm ci)
fi
(cd console && npm run build)

rm -rf web/console/assets web/console/sponsor
mkdir -p web/console
cp console/dist/index.html console/dist/favicon.svg web/console/
cp -R console/dist/assets web/console/
if [ -d console/dist/sponsor ]; then
  cp -R console/dist/sponsor web/console/
fi

echo "控制台已更新：web/console/"
