package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const TeacherContentAnchorSchema = "teacher-content-anchor.v1"

var teacherContentDigest = regexp.MustCompile("^sha256:[a-f0-9]{64}$")

// TeacherContentAnchor identifies a source object. It grants no access;
// Teacher must ask the content owner to authorize and verify the pin.
type TeacherContentAnchor struct {
	Schema           string `json:"schema"`
	Kind             string `json:"kind"`
	CourseID         string `json:"course_id,omitempty"`
	ModuleID         string `json:"module_id,omitempty"`
	LessonID         string `json:"lesson_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	ArtifactID       string `json:"artifact_id,omitempty"`
	ContentRevision  string `json:"content_revision,omitempty"`
	ContentDigest    string `json:"content_digest,omitempty"`
	LearningActionID string `json:"learning_action_id,omitempty"`
	LanguageCode     string `json:"language_code,omitempty"`
}

func validAnchorUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func (a TeacherContentAnchor) Valid() bool {
	if a.Schema != TeacherContentAnchorSchema ||
		(a.LanguageCode != "" && a.LanguageCode != "en" && a.LanguageCode != "ru") ||
		(a.ContentDigest == "" && a.ContentRevision == "") ||
		(a.ContentDigest != "" && !teacherContentDigest.MatchString(a.ContentDigest)) {
		return false
	}
	if a.ContentRevision != "" && !validAnchorUUID(a.ContentRevision) {
		timestamp, err := time.Parse(time.RFC3339Nano, a.ContentRevision)
		if err != nil || timestamp.UTC().Format(time.RFC3339Nano) != a.ContentRevision {
			return false
		}
	}
	for _, id := range []string{a.CourseID, a.ModuleID, a.LessonID, a.TaskID, a.ArtifactID, a.LearningActionID} {
		if id != "" && !validAnchorUUID(id) {
			return false
		}
	}
	switch a.Kind {
	case "COURSE":
		return a.CourseID != "" && a.ModuleID == "" && a.LessonID == "" && a.TaskID == "" && a.ArtifactID == "" && a.LearningActionID == ""
	case "MODULE":
		return a.CourseID != "" && a.ModuleID != "" && a.LessonID == "" && a.TaskID == "" && a.ArtifactID == "" && a.LearningActionID == ""
	case "LESSON":
		return a.CourseID != "" && a.LessonID != "" && a.TaskID == "" && a.ArtifactID == "" && a.LearningActionID == ""
	case "EXAMPLE":
		return a.CourseID != "" && a.LessonID != "" && a.TaskID == "" && a.ArtifactID != "" && a.LearningActionID == ""
	case "TEST":
		return a.CourseID != "" && a.LessonID != "" && a.TaskID != "" && a.ArtifactID != ""
	case "LESSON_TASK":
		return a.CourseID != "" && a.LessonID != "" && a.TaskID == "" && a.ArtifactID != "" && a.LearningActionID != ""
	case "PRACTICE_TASK", "PROJECT":
		return a.LanguageCode == "" && a.CourseID == "" && a.ModuleID == "" && a.LessonID == "" && a.TaskID == "" && a.ArtifactID != "" && a.LearningActionID != ""
	default:
		return false
	}
}

func (a *TeacherContentAnchor) UnmarshalJSON(raw []byte) error {
	if len(raw) > 768 {
		return ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return ErrValidation
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return ErrValidation
		}
		seen[key] = true
		switch key {
		case "schema", "kind", "course_id", "module_id", "lesson_id", "task_id", "artifact_id", "content_revision", "content_digest", "learning_action_id", "language_code":
		default:
			return ErrValidation
		}
		token, err = d.Token()
		value, ok := token.(string)
		if err != nil || !ok || value == "" {
			return ErrValidation
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return ErrValidation
	}
	if _, err = d.Token(); err != io.EOF {
		return ErrValidation
	}
	type wire TeacherContentAnchor
	var value wire
	if json.Unmarshal(raw, &value) != nil || !TeacherContentAnchor(value).Valid() {
		return ErrValidation
	}
	*a = TeacherContentAnchor(value)
	return nil
}

func TeacherContentAnchorsEqual(first, second *TeacherContentAnchor) bool {
	if first == nil || second == nil {
		return first == second
	}
	return *first == *second
}

func ValidateTeacherContentAnchorBinding(dialog Dialog, anchor *TeacherContentAnchor, actionID *uuid.UUID, lesson *LessonMessageContext) error {
	if anchor == nil {
		return nil
	}
	invalid := fmt.Errorf("%w: content anchor does not match teacher dialog", ErrValidation)
	if !anchor.Valid() || dialog.Type != DialogTypeTeacher || anchor.LearningActionID != anchorUUIDString(actionID) {
		return invalid
	}
	switch dialog.TeacherContextType {
	case TeacherContextGeneralTeacher:
		if anchor.Kind == "COURSE" || anchor.Kind == "MODULE" {
			return nil
		}
	case TeacherContextLesson:
		if lesson == nil || anchor.LessonID != anchorUUIDString(dialog.ContextID) ||
			anchor.CourseID != anchorUUIDString(lesson.CourseID) || anchor.LessonID != anchorUUIDString(lesson.LessonID) {
			return invalid
		}
		if anchor.Kind == "LESSON" && anchor.ContentRevision == lesson.ContentRevision &&
			(lesson.ContentDigest == "" || anchor.ContentDigest == lesson.ContentDigest) {
			return nil
		}
		if lesson.Mode == LessonMessageContextOverview && (anchor.Kind == "EXAMPLE" || anchor.Kind == "TEST") {
			return nil
		}
	case TeacherContextLessonTask:
		if anchor.Kind == "LESSON_TASK" && anchor.ArtifactID == anchorUUIDString(dialog.ContextID) ||
			anchor.Kind == "TEST" && anchor.TaskID == anchorUUIDString(dialog.ContextID) {
			return nil
		}
	case TeacherContextPracticeTask:
		if anchor.Kind == "PRACTICE_TASK" && anchor.ArtifactID == anchorUUIDString(dialog.ContextID) {
			return nil
		}
	case TeacherContextProject:
		if anchor.Kind == "PROJECT" && anchor.ArtifactID == anchorUUIDString(dialog.ContextID) {
			return nil
		}
	}
	return invalid
}

func anchorUUIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
