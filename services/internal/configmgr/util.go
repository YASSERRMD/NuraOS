package configmgr

import "bytes"

// jsonLinesReader wraps a byte slice in an io.Reader for json.Decoder.
func jsonLinesReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
