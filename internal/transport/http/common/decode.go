package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const MaxJSONBodyBytes = 1 << 20

func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return DecodeJSONLimit(w, r, target, MaxJSONBodyBytes)
}

func DecodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, maximum int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
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

func DecodeOptionalEmptyBody(r *http.Request) error {
	if r.Body == nil {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil {
		return err
	}
	if len(payload) > 1024 {
		return fmt.Errorf("request body exceeds 1024 bytes")
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var empty map[string]json.RawMessage
	if err := decoder.Decode(&empty); err != nil {
		return err
	}
	if empty == nil || len(empty) != 0 {
		return fmt.Errorf("request body must be empty")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain at most one empty JSON object")
	}
	return nil
}
