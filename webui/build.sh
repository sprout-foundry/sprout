#!/bin/bash

set -euo pipefail

echo "🏗️  Building React Web UI..."

# Check if node_modules exists
if [ ! -d "node_modules" ]; then
    echo "📦 Installing dependencies..."
    npm install
fi

echo "🔨 Building React app..."
npm run build

# Copy build to the pkg/webui/static directory for embedding
echo "📁 Copying build assets to Go package..."
target_dir="../pkg/webui/static"
mkdir -p "$target_dir"

# Clear the previous embedded bundle while keeping the target directory itself.
find "$target_dir" -mindepth 1 -maxdepth 1 -exec rm -rf {} +

# Copy root-level build assets first, then flatten CRA's nested static assets
# into the Go-embedded layout expected by /static/* handlers.
find build -mindepth 1 -maxdepth 1 ! -name static -exec cp -R {} "$target_dir"/ \;
if [ -d "build/static" ]; then
    cp -R build/static/. "$target_dir"/
fi

echo "✅ React Web UI build completed!"
echo "📊 Build size:"
du -sh build/
echo "🎨 Assets ready for embedding in Go binary"
