package domain

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"io"
)

// TeacherPolicySelection is untrusted pedagogical intent attached to one
// Student-authored Teacher message. Only Mentor may authorize the selection.
// It is independent of human Mentor dialog membership and grants no access.
type TeacherPolicySelection struct {
	Kind         string `json:"kind"`
	OfferingID   string `json:"offering_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
	CohortID     string `json:"cohort_id,omitempty"`
	EnrollmentID string `json:"enrollment_id,omitempty"`
}

func (s TeacherPolicySelection) Valid() bool {
	valid := func(v string) bool { u, e := uuid.Parse(v); return e == nil && u != uuid.Nil && u.String() == v }
	return s.Kind == "OFFER" && valid(s.OfferingID) && valid(s.AssignmentID) && s.CohortID == "" && s.EnrollmentID == "" || s.Kind == "COHORT" && valid(s.CohortID) && valid(s.EnrollmentID) && s.OfferingID == "" && s.AssignmentID == ""
}
func (s *TeacherPolicySelection) UnmarshalJSON(raw []byte) error {
	if len(raw) > 512 {
		return ErrValidation
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, e := decoder.Token()
	if e != nil || token != json.Delim('{') {
		return ErrValidation
	}
	fields := map[string]string{}
	for decoder.More() {
		token, e = decoder.Token()
		key, ok := token.(string)
		if e != nil || !ok {
			return ErrValidation
		}
		if _, exists := fields[key]; exists {
			return ErrValidation
		}
		switch key {
		case "kind", "offering_id", "assignment_id", "cohort_id", "enrollment_id":
		default:
			return ErrValidation
		}
		token, e = decoder.Token()
		value, ok := token.(string)
		if e != nil || !ok {
			return ErrValidation
		}
		fields[key] = value
	}
	if token, e = decoder.Token(); e != nil || token != json.Delim('}') {
		return ErrValidation
	}
	if _, e = decoder.Token(); e != io.EOF {
		return ErrValidation
	}
	value := TeacherPolicySelection{Kind: fields["kind"], OfferingID: fields["offering_id"], AssignmentID: fields["assignment_id"], CohortID: fields["cohort_id"], EnrollmentID: fields["enrollment_id"]}
	if len(fields) != 3 || !value.Valid() {
		return ErrValidation
	}
	*s = value
	return nil
}
func TeacherPolicySelectionsEqual(a, b *TeacherPolicySelection) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func ValidateTeacherPolicySelectionBinding(dialog Dialog, selection *TeacherPolicySelection, lesson *LessonMessageContext) error {
	if selection == nil {
		return nil
	}
	if !selection.Valid() || dialog.Type != DialogTypeTeacher {
		return ErrValidation
	}
	switch dialog.TeacherContextType {
	case TeacherContextPracticeTask, TeacherContextProject:
		return nil
	case TeacherContextLesson:
		if lesson != nil && lesson.Schema == LessonMessageContextSchemaV2 {
			return nil
		}
	}
	return ErrValidation
}
