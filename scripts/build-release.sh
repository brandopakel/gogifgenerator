#!/bin/sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
distribution_directory="$repository_root/dist"
mkdir -p "$distribution_directory"

build_server() {
	operating_system=$1
	architecture=$2
	extension=$3
	output="$distribution_directory/gogif-$operating_system-$architecture$extension"
	CGO_ENABLED=0 GOOS="$operating_system" GOARCH="$architecture" \
		go build -trimpath -ldflags="-s -w" -o "$output" ./cmd/gogif
}

cd "$repository_root"
build_server darwin arm64 ""
build_server darwin amd64 ""
build_server windows amd64 ".exe"
build_server linux amd64 ""
build_server linux arm64 ""

for browser in chrome edge firefox; do
	artifact="$distribution_directory/gogif-$browser-extension.zip"
	(
		cd apps/extension
		zip -q -FS "$artifact" manifest.json popup.html popup.js popup.css README.md icons/*.png
	)
done

shasum -a 256 \
	"$distribution_directory/gogif-darwin-arm64" \
	"$distribution_directory/gogif-darwin-amd64" \
	"$distribution_directory/gogif-windows-amd64.exe" \
	"$distribution_directory/gogif-linux-amd64" \
	"$distribution_directory/gogif-linux-arm64" \
	"$distribution_directory/gogif-chrome-extension.zip" \
	"$distribution_directory/gogif-edge-extension.zip" \
	"$distribution_directory/gogif-firefox-extension.zip" \
	> "$distribution_directory/SHA256SUMS"

echo "GoGIF builds are ready in $distribution_directory"
