#!/bin/bash

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
rm -rf ../pkg/webui/static/*
cp -r build/* ../pkg/webui/static/
# Move nested static contents up one level to avoid double nesting
if [ -d "../pkg/webui/static/static" ]; then
    mv ../pkg/webui/static/static/* ../pkg/webui/static/
    rmdir ../pkg/webui/static/static
fi

echo "✅ React Web UI build completed!"
echo "📊 Build size:"
du -sh build/
echo "🎨 Assets ready for embedding in Go binary"