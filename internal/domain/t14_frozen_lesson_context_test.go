package domain

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestT14FrozenLessonContextAccepted(t *testing.T) {
	input := []byte(`{"schema":"lesson-message-context.v2","mode":"lesson_overview","course_id":"11111111-1111-4111-8111-111111111111","lesson_id":"22222222-2222-4222-8222-222222222222","content_revision":"33333333-3333-4333-8333-333333333333","learning_path_id":"44444444-4444-4444-8444-444444444444","learning_path_item_id":"55555555-5555-4555-8555-555555555555","content_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	var context LessonMessageContext
	if err := json.Unmarshal(input, &context); err != nil {
		t.Fatalf("frozen UUID context rejected: %v", err)
	}
	normalized, err := NormalizeLessonMessageContext(&context)
	if err != nil {
		t.Fatalf("frozen UUID context rejected: %v", err)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip LessonMessageContext
	if err := json.Unmarshal(encoded, &roundtrip); err != nil || !LessonMessageContextsEqual(normalized, &roundtrip) {
		t.Fatalf("anchor lost on JSON roundtrip: %s %v", encoded, err)
	}
}

func frozenT14Context() *LessonMessageContext {
	course, lesson, path, item := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	return &LessonMessageContext{Schema: LessonMessageContextSchemaV2, Mode: LessonMessageContextOverview,
		CourseID: &course, LessonID: &lesson, ContentRevision: uuid.NewString(), LearningPathID: &path,
		LearningPathItemID: &item, ContentDigest: "sha256:" + strings.Repeat("a", 64)}
}

func TestT14FrozenContextStrictWire(t *testing.T) {
	original, _ := json.Marshal(frozenT14Context())
	var base map[string]any
	if err := json.Unmarshal(original, &base); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "mode", "course_id", "lesson_id", "content_revision", "learning_path_id", "learning_path_item_id", "content_digest"} {
		for _, mutation := range []string{"missing", "null"} {
			t.Run(key+mutation, func(t *testing.T) {
				candidate := make(map[string]any)
				for k, v := range base {
					candidate[k] = v
				}
				if mutation == "missing" {
					delete(candidate, key)
				} else {
					candidate[key] = nil
				}
				raw, _ := json.Marshal(candidate)
				var decoded LessonMessageContext
				if err := json.Unmarshal(raw, &decoded); err == nil {
					t.Fatalf("accepted %s", raw)
				}
			})
		}
	}
	for name, changes := range map[string]map[string]any{
		"unknown": {"private_answer": "x"}, "overview_selection": {"selected_text": "x"},
		"overview_null_selection": {"selected_text": nil}, "timestamp": {"content_revision": "2026-09-04T08:00:00Z"},
		"zero_revision": {"content_revision": uuid.Nil.String()}, "digest_case": {"content_digest": "sha256:" + strings.Repeat("A", 64)},
		"v1_cross_schema":   {"schema": LessonMessageContextSchemaV1, "content_revision": "2026-09-04T08:00:00Z"},
		"selection_missing": {"mode": "selection"}, "selection_blank": {"mode": "selection", "selected_text": " \n"},
		"selection_overflow": {"mode": "selection", "selected_text": strings.Repeat("界", 12001)},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := make(map[string]any)
			for k, v := range base {
				candidate[k] = v
			}
			for k, v := range changes {
				candidate[k] = v
			}
			raw, _ := json.Marshal(candidate)
			var decoded LessonMessageContext
			if err := json.Unmarshal(raw, &decoded); err == nil {
				t.Fatal("accepted malformed context")
			}
		})
	}
	selection := " \n" + strings.Repeat("界", 11996) + "\n "
	input := frozenT14Context()
	input.Mode = LessonMessageContextSelection
	input.SelectedText = &selection
	normalized, err := NormalizeLessonMessageContext(input)
	if err != nil || normalized.SelectedText == nil || *normalized.SelectedText != selection {
		t.Fatalf("exact 12000-rune selection changed: %v", err)
	}
}

func TestT14FrozenContextEqualityEveryAnchor(t *testing.T) {
	original := frozenT14Context()
	for name, mutate := range map[string]func(*LessonMessageContext){
		"path":     func(c *LessonMessageContext) { id := uuid.New(); c.LearningPathID = &id },
		"item":     func(c *LessonMessageContext) { id := uuid.New(); c.LearningPathItemID = &id },
		"digest":   func(c *LessonMessageContext) { c.ContentDigest = "sha256:" + strings.Repeat("b", 64) },
		"revision": func(c *LessonMessageContext) { c.ContentRevision = uuid.NewString() },
		"course":   func(c *LessonMessageContext) { id := uuid.New(); c.CourseID = &id },
		"lesson":   func(c *LessonMessageContext) { id := uuid.New(); c.LessonID = &id },
	} {
		t.Run(name, func(t *testing.T) {
			copy, err := NormalizeLessonMessageContext(original)
			if err != nil {
				t.Fatal(err)
			}
			mutate(copy)
			if LessonMessageContextsEqual(original, copy) {
				t.Fatal("changed anchor equal")
			}
		})
	}
}

func TestT14FrozenContextRejectsDuplicateAndCaseAliasKeys(t *testing.T) {
	anchor := frozenT14Context()
	raw, err := json.Marshal(anchor)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "mode", "course_id", "lesson_id", "content_revision", "learning_path_id", "learning_path_item_id", "content_digest"} {
		t.Run("alias_"+key, func(t *testing.T) {
			candidate := strings.Replace(string(raw), `"`+key+`":`, `"`+strings.ToUpper(key)+`":`, 1)
			var decoded LessonMessageContext
			if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
				t.Fatalf("accepted case alias %s", key)
			}
		})
		t.Run("duplicate_"+key, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			candidate := string(raw[:len(raw)-1]) + `,"` + key + `":` + string(fields[key]) + `}`
			var decoded LessonMessageContext
			if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
				t.Fatalf("accepted duplicate %s", key)
			}
		})
	}
	candidate := string(raw[:len(raw)-1]) + `,"learning_path_item_id":"` + uuid.NewString() + `"}`
	var decoded LessonMessageContext
	if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
		t.Fatal("accepted conflicting item identities")
	}
	alias := strings.Replace(string(raw), `"learning_path_id":`, `"Learning_Path_ID":`, 1)
	if err := json.Unmarshal([]byte(alias), &decoded); err == nil {
		t.Fatal("accepted mixed-case path alias")
	}
	extraAlias := string(raw[:len(raw)-1]) + `,"Learning_Path_ID":"` + anchor.LearningPathID.String() + `"}`
	if err := json.Unmarshal([]byte(extraAlias), &decoded); err == nil {
		t.Fatal("accepted extra alias beyond exact V2 cardinality")
	}
	// Both selected_text duplicates and case aliases must remain invalid.
	anchor.Mode = LessonMessageContextSelection
	selection := "exact"
	anchor.SelectedText = &selection
	raw, err = json.Marshal(anchor)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{strings.Replace(string(raw), `"selected_text":`, `"Selected_Text":`, 1), string(raw[:len(raw)-1]) + `,"selected_text":"changed"}`} {
		if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
			t.Fatal("accepted ambiguous selection key")
		}
	}
}

func TestT14FrozenContextRejectsInvalidUTF8Wire(t *testing.T) {
	anchor := frozenT14Context()
	anchor.Mode = LessonMessageContextSelection
	selection := "exact"
	anchor.SelectedText = &selection
	raw, err := json.Marshal(anchor)
	if err != nil {
		t.Fatal(err)
	}
	candidate := []byte(strings.Replace(string(raw), `"selected_text":"exact"`, `"selected_text":"`+string([]byte{0xff})+`"`, 1))
	var decoded LessonMessageContext
	if err := json.Unmarshal(candidate, &decoded); err == nil {
		t.Fatal("invalid UTF8 wire selection silently repaired")
	}
}
