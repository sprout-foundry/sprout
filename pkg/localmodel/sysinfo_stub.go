//go:build !linux && !darwin

package localmodel

func tensorTotalSystemRAM() uint64 {
	return 0
}
