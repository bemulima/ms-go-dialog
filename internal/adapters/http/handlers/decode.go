package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const maxJSONBodyBytes = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain exactly one JSON object")
	}
	return nil
}

func decodeOptionalEmptyBody(r *http.Request) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024))
	var empty map[string]json.RawMessage
	if err := decoder.Decode(&empty); err != nil {
		return err
	}
	if len(empty) != 0 {
		return fmt.Errorf("request body must be empty")
	}
	return nil
}
