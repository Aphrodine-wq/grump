#!/usr/bin/env bash
set -euo pipefail

VERSION=$(node -p "require('./package.json').version")
BINARY_NAME="grump"
MODULE="g-rump-cli"
DIST_DIR="dist"

PLATFORMS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

echo "Building ${BINARY_NAME} v${VERSION} for ${#PLATFORMS[@]} platforms..."
echo ""

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"

  output_name="${BINARY_NAME}"
  if [ "${GOOS}" = "windows" ]; then
    output_name="${output_name}.exe"
  fi

  archive_name="${BINARY_NAME}_v${VERSION}_${GOOS}_${GOARCH}"
  build_dir="${DIST_DIR}/${archive_name}"

  mkdir -p "${build_dir}"

  echo "  Building ${GOOS}/${GOARCH}..."

  GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=0 \
    go build -ldflags="-s -w -X ${MODULE}/cmd.version=${VERSION}" \
    -o "${build_dir}/${output_name}" .

  # Create archive
  cd "${DIST_DIR}"
  if [ "${GOOS}" = "windows" ]; then
    zip -q "${archive_name}.zip" -r "${archive_name}/"
  else
    tar -czf "${archive_name}.tar.gz" "${archive_name}/"
  fi
  cd ..

  echo "  -> ${DIST_DIR}/${archive_name}.tar.gz"
done

echo ""
echo "Done! Archives are in ${DIST_DIR}/"
echo ""
echo "To create a GitHub release:"
echo "  gh release create v${VERSION} ${DIST_DIR}/*.tar.gz ${DIST_DIR}/*.zip --title \"v${VERSION}\""
