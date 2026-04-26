package codec

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
)

func DecodeBody(content []byte, contentEncoding string) string {
	data := content
	if strings.Contains(strings.ToLower(contentEncoding), "gzip") {
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err == nil {
			decompressed, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr == nil {
				data = decompressed
			}
		}
	}
	return string(data)
}

func EncodeBody(text string, contentEncoding string) []byte {
	if !strings.Contains(strings.ToLower(contentEncoding), "gzip") {
		return []byte(text)
	}

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write([]byte(text))
	_ = writer.Close()
	return buffer.Bytes()
}
