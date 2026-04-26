package codec

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
)

const DefaultMaxDecodeDepth = 4

type Match struct {
	Name  string
	Value string
}

type Scanner interface {
	ScanText(text string) []Match
}

type Replacer interface {
	Mask(value, name string) string
}

type Restorer interface {
	Restore(text string) string
}

type BlobStrategy int

const (
	EmbeddedTokens BlobStrategy = iota
	WholeBlobTokens
)

type EncodedBlobOptions struct {
	MaxDepth int
	Strategy BlobStrategy
}

var (
	base64RE = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
	hexRE    = regexp.MustCompile(`[0-9a-fA-F]{40,}`)
	urlRE    = regexp.MustCompile(`((?:[A-Za-z0-9._~\-]|%[0-9a-fA-F]{2}){12,}%[0-9a-fA-F]{2}(?:[A-Za-z0-9._~\-]|%[0-9a-fA-F]{2})*)`)
)

type encodedMatch struct {
	start    int
	end      int
	value    string
	encoding string
}

func RewriteEncodedBlobs(text string, scanner Scanner, replacer Replacer, options EncodedBlobOptions) (string, int) {
	options = normalizeEncodedOptions(options)
	total := 0
	current := text

	for depth := 0; depth < options.MaxDepth; depth++ {
		next, masked := rewritePass(current, scanner, replacer, options, options.MaxDepth-depth)
		current = next
		total += masked
		if masked == 0 {
			break
		}
	}

	return current, total
}

func RestoreEncodedBlobs(text string, restorer Restorer, options EncodedBlobOptions) string {
	options = normalizeEncodedOptions(options)
	return restorePass(text, restorer, options.MaxDepth)
}

func rewritePass(text string, scanner Scanner, replacer Replacer, options EncodedBlobOptions, depth int) (string, int) {
	total := 0
	current := text

	for _, candidate := range findEncodedMatches(current) {
		decoded, ok := decodeBlob(candidate.value, candidate.encoding)
		if !ok {
			continue
		}

		rewritten := decoded
		innerMasked := 0
		if depth > 1 {
			rewritten, innerMasked = rewritePass(rewritten, scanner, replacer, options, depth-1)
		}

		hits := scanner.ScanText(rewritten)
		if len(hits) == 0 && innerMasked == 0 {
			continue
		}

		total += innerMasked

		var replacement string
		switch options.Strategy {
		case WholeBlobTokens:
			replacement = replacer.Mask(candidate.value, "ENCODED_BLOB")
			total++
		default:
			maskedDecoded, hitCount := maskHits(rewritten, hits, replacer)
			replacement = encodeBlob(maskedDecoded, candidate.encoding)
			total += hitCount
		}

		current = current[:candidate.start] + replacement + current[candidate.end:]
	}

	return current, total
}

func restorePass(text string, restorer Restorer, depth int) string {
	current := restorer.Restore(text)
	if depth <= 0 {
		return current
	}

	for _, candidate := range findEncodedMatches(current) {
		decoded, ok := decodeBlob(candidate.value, candidate.encoding)
		if !ok {
			continue
		}
		restoredDecoded := restorePass(decoded, restorer, depth-1)
		if restoredDecoded == decoded {
			continue
		}
		reEncoded := encodeBlob(restoredDecoded, candidate.encoding)
		current = current[:candidate.start] + reEncoded + current[candidate.end:]
	}

	return current
}

func maskHits(text string, hits []Match, replacer Replacer) (string, int) {
	current := text
	count := 0
	for _, hit := range hits {
		if !strings.Contains(current, hit.Value) {
			continue
		}
		current = strings.ReplaceAll(current, hit.Value, replacer.Mask(hit.Value, hit.Name))
		count++
	}
	return current, count
}

func normalizeEncodedOptions(options EncodedBlobOptions) EncodedBlobOptions {
	if options.MaxDepth <= 0 {
		options.MaxDepth = DefaultMaxDecodeDepth
	}
	return options
}

func findEncodedMatches(text string) []encodedMatch {
	type finder struct {
		re       *regexp.Regexp
		encoding string
	}

	findings := []finder{
		{re: urlRE, encoding: "url"},
		{re: base64RE, encoding: "base64"},
		{re: hexRE, encoding: "hex"},
	}

	var matches []encodedMatch
	for _, finding := range findings {
		indexes := finding.re.FindAllStringSubmatchIndex(text, -1)
		for i := len(indexes) - 1; i >= 0; i-- {
			index := indexes[i]
			start, end := index[0], index[1]
			if len(index) >= 4 && index[2] >= 0 && index[3] >= 0 {
				start, end = index[2], index[3]
			}
			if !hasTokenBoundary(text, start, end, finding.encoding) {
				continue
			}
			matches = append(matches, encodedMatch{
				start:    start,
				end:      end,
				value:    text[start:end],
				encoding: finding.encoding,
			})
		}
	}
	return matches
}

func hasTokenBoundary(text string, start, end int, encoding string) bool {
	leftOK := start == 0
	if !leftOK {
		b := text[start-1]
		// For base64, '=' is only used as padding at the END of a base64 string —
		// it never appears before one. A '=' immediately to the left (e.g. the
		// key=value separator in "base64_url=BASE64VALUE") is not part of the blob,
		// so treat it as a valid left boundary.
		if encoding == "base64" && b == '=' {
			leftOK = true
		} else {
			leftOK = !isTokenChar(b, encoding)
		}
	}

	rightOK := end == len(text)
	if !rightOK {
		rightOK = !isTokenChar(text[end], encoding)
	}

	return leftOK && rightOK
}

func isTokenChar(b byte, encoding string) bool {
	switch encoding {
	case "base64":
		return (b >= 'A' && b <= 'Z') ||
			(b >= 'a' && b <= 'z') ||
			(b >= '0' && b <= '9') ||
			b == '+' || b == '/' || b == '='
	case "hex":
		return (b >= 'A' && b <= 'F') ||
			(b >= 'a' && b <= 'f') ||
			(b >= '0' && b <= '9')
	default:
		return false
	}
}

func decodeBlob(blob, encoding string) (string, bool) {
	switch encoding {
	case "url":
		decoded, err := url.PathUnescape(blob)
		return decoded, err == nil && decoded != blob
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(padBase64(blob))
		return string(decoded), err == nil
	case "hex":
		if len(blob)%2 != 0 {
			return "", false
		}
		decoded, err := hex.DecodeString(blob)
		return string(decoded), err == nil
	default:
		return "", false
	}
}

func encodeBlob(value, encoding string) string {
	switch encoding {
	case "url":
		return escapeAll(value)
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(value))
	case "hex":
		return hex.EncodeToString([]byte(value))
	default:
		return value
	}
}

func padBase64(blob string) string {
	switch len(blob) % 4 {
	case 2:
		return blob + "=="
	case 3:
		return blob + "="
	default:
		return blob
	}
}

func escapeAll(value string) string {
	escaped := url.QueryEscape(value)
	return strings.ReplaceAll(escaped, "+", "%20")
}
