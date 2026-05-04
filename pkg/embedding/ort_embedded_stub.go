//go:build !embed_ort

package embedding

func getEmbeddedORT() []byte {
	return nil
}
