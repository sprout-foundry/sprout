package embedding

import _ "embed"

//go:embed models/all-MiniLM-L6-v2-int8.onnx
var modelONNX []byte

//go:embed models/tokenizer.json
var embeddedTokenizerJSON []byte
