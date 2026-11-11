#!/usr/bin/env bash
#
# SPDX-FileCopyrightText: Contributors to the Gardener project
#
# SPDX-License-Identifier: Apache-2.0

set -e

echo "> Installing gosec"

TOOLS_BIN_DIR=${TOOLS_BIN_DIR:-$(dirname $0)/bin}

platform=$(uname -s | tr '[:upper:]' '[:lower:]')
version=$GOSEC_VERSION
case $(uname -m) in
  aarch64 | arm64)
    arch="arm64"
    ;;
  x86_64)
    arch="amd64"
    ;;
  *)
    echo "Unknown architecture"
    exit -1
    ;;
esac

archive_name="gosec_${version#v}_${platform}_${arch}"
file_name="${archive_name}.tar.gz"
checksums_name="gosec_${version#v}_checksums.txt"

temp_dir="$(mktemp -d)"
function cleanup {
  rm -rf "${temp_dir}"
}
trap cleanup EXIT ERR INT TERM

base_url="https://github.com/securego/gosec/releases/download/${version}"
curl -L -o "${temp_dir}/${file_name}" "${base_url}/${file_name}"
curl -L -o "${temp_dir}/${checksums_name}" "${base_url}/${checksums_name}"

# Verify the downloaded archive against the checksums published alongside it before
# extracting. gosec authors the SAST attestation attached to our component descriptor,
# so a tampered binary would otherwise be trusted end to end.
echo "> Verifying checksum of ${file_name}"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "${temp_dir}" && grep " ${file_name}\$" "${checksums_name}" | sha256sum --check --strict -)
else
  (cd "${temp_dir}" && grep " ${file_name}\$" "${checksums_name}" | shasum -a 256 -c -)
fi

tar -xzm -C "${temp_dir}" -f "${temp_dir}/${file_name}"
mv "${temp_dir}/gosec" $TOOLS_BIN_DIR
chmod +x $TOOLS_BIN_DIR/gosec

