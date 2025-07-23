package editor

func testableFileTypes() map[string]bool {
	return map[string]bool{
		".go":   true,
		".py":   true,
		".js":   true,
		".ts":   true,
		".java": true,
		".c":    true,
		".cpp":  true,
		".cs":   true,
		".rb":   true,
		".php":  true,
		".sh":   false, // ignore testing shell scripts
	}
}
