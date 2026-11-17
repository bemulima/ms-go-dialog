package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

// DecodeExactJSON is for the additive flat command contracts. It rejects
// duplicate/case-aliased fields, null, invalid UTF-8 and missing members before
// decoding typed values. Existing legacy commands retain their own decoder.
func DecodeExactJSON(w http.ResponseWriter, r *http.Request, target any, maximum int64, fields ...string) error {
	if len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) != 0 {
		return fmt.Errorf("invalid content type")
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return fmt.Errorf("invalid content type")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maximum))
	if err != nil || !utf8.Valid(raw) {
		return fmt.Errorf("invalid JSON bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("object required")
	}
	allowed := map[string]bool{}
	for _, field := range fields {
		allowed[field] = true
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return fmt.Errorf("invalid field")
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("invalid value")
		}
		if key == "id" || strings.HasSuffix(key, "_id") {
			var rawID string
			if json.Unmarshal(value, &rawID) != nil {
				return fmt.Errorf("invalid UUID")
			}
			id, err := uuid.Parse(rawID)
			if err != nil || id == uuid.Nil || id.String() != rawID {
				return fmt.Errorf("invalid UUID")
			}
		}
	}
	if _, err = decoder.Token(); err != nil || len(seen) != len(fields) {
		return fmt.Errorf("missing field")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return json.Unmarshal(raw, target)
}
