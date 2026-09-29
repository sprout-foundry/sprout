package webui

import (
	"mime"
	"strings"
)

// webAssetTypes pins the Content-Type of what the UI bundle ships. On
// Windows mime.TypeByExtension consults the registry, which installers
// often rewrite (".js" as "text/plain" is common); browsers then refuse
// to execute the module scripts and the UI loads blank.
var webAssetTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".json":  "application/json",
	".map":   "application/json",
	".wasm":  "application/wasm",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".txt":   "text/plain; charset=utf-8",
}

func assetContentType(ext string) string {
	if ct, ok := webAssetTypes[strings.ToLower(ext)]; ok {
		return ct
	}
	return mime.TypeByExtension(ext)
}
