#!/usr/bin/env bash

set -e

archs=(amd64 arm64)
oses=(linux darwin windows)

APP_NAME="x-ui"

mkdir -p dist

for os in "${oses[@]}"; do
  for arch in "${archs[@]}"; do

    output_name="${APP_NAME}-${os}-${arch}"
    build_path="dist/${output_name}"

    echo "Building $output_name..."

    # Windows needs .exe
    if [ "$os" = "windows" ]; then
      bin_name="${APP_NAME}.exe"
    else
      bin_name="${APP_NAME}"
    fi

    mkdir -p "$build_path"

    # Build binary
    GOOS=$os GOARCH=$arch go build -o "$build_path/$bin_name"

    # Package
    cd dist

    if [ "$os" = "windows" ]; then
      zip -r "${output_name}.zip" "$output_name"
    else
      tar -czf "${output_name}.tar.gz" "$output_name"
    fi

    # Cleanup folder (optional, like releases usually do)
    rm -rf "$output_name"

    cd - > /dev/null

  done
done

echo "Done. Files are in ./dist"