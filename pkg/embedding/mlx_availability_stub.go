//go:build !darwin || !arm64 || !cgo || !mlx

package embedding

func mlxProviderAvailable() bool { return false }
