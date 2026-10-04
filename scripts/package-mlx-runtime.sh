#!/bin/sh
# Packages Apple's MLX runtime (libmlxc, libmlx, mlx.metallib) as a
# self-contained archive that sprout downloads on demand for local models,
# so Macs without Homebrew can run them. Run on Apple Silicon with the
# Homebrew mlx and mlx-c formulas installed.
#
#   scripts/package-mlx-runtime.sh [output.tar.gz]
#
# The libraries are rewired to find each other in their own directory
# (@loader_path) instead of Homebrew's prefix, then re-signed (ad hoc).
set -eu

out=${1:-sprout-mlx-runtime-darwin-arm64.tar.gz}

if [ "$(uname -s)" != Darwin ] || [ "$(uname -m)" != arm64 ]; then
	echo "error: the MLX runtime is packaged on Apple Silicon only" >&2
	exit 1
fi
command -v brew >/dev/null 2>&1 || { echo "error: Homebrew is required to package the runtime" >&2; exit 1; }

mlx_prefix=$(brew --prefix mlx)
mlxc_prefix=$(brew --prefix mlx-c)
for f in "$mlxc_prefix/lib/libmlxc.dylib" "$mlx_prefix/lib/libmlx.dylib" "$mlx_prefix/lib/mlx.metallib"; do
	[ -f "$f" ] || { echo "error: missing $f (brew install mlx mlx-c)" >&2; exit 1; }
done

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
dir="$stage/mlx-runtime"
mkdir -p "$dir/licenses"

cp "$mlxc_prefix/lib/libmlxc.dylib" "$mlx_prefix/lib/libmlx.dylib" "$mlx_prefix/lib/mlx.metallib" "$dir/"
chmod u+w "$dir"/*.dylib
cp "$mlx_prefix/LICENSE" "$dir/licenses/mlx-LICENSE"
cp "$mlxc_prefix/LICENSE" "$dir/licenses/mlx-c-LICENSE"

# libmlxc links libmlx by its Homebrew path; point it at the copy beside it.
old_dep=$(otool -L "$dir/libmlxc.dylib" | awk '/libmlx\.dylib/ {print $1; exit}')
[ -n "$old_dep" ] || { echo "error: libmlxc.dylib does not reference libmlx.dylib" >&2; exit 1; }
install_name_tool -id @loader_path/libmlx.dylib "$dir/libmlx.dylib"
install_name_tool -id @loader_path/libmlxc.dylib "$dir/libmlxc.dylib"
install_name_tool -change "$old_dep" @loader_path/libmlx.dylib "$dir/libmlxc.dylib"
codesign --force --sign - "$dir/libmlx.dylib"
codesign --force --sign - "$dir/libmlxc.dylib"

if otool -L "$dir/libmlxc.dylib" "$dir/libmlx.dylib" | grep -E "/opt/homebrew|/usr/local/opt"; then
	echo "error: the packaged libraries still reference Homebrew paths" >&2
	exit 1
fi

{
	echo "mlx $(brew list --versions mlx | awk '{print $2}')"
	echo "mlx-c $(brew list --versions mlx-c | awk '{print $2}')"
} >"$dir/VERSIONS"

tar -czf "$out" -C "$stage" mlx-runtime
echo "packaged $out ($(du -h "$out" | awk '{print $1}'))"
