package domain

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestTeacherPolicySelectionClosedExactUnion(t *testing.T) {
	a, b := uuid.NewString(), uuid.NewString()
	good := `{"kind":"OFFER","offering_id":"` + a + `","assignment_id":"` + b + `"}`
	for _, raw := range []string{good, `{"kind":"COHORT","cohort_id":"` + a + `","enrollment_id":"` + b + `"}`} {
		var s TeacherPolicySelection
		if e := json.Unmarshal([]byte(raw), &s); e != nil || !s.Valid() {
			t.Fatalf("valid intent rejected %v", e)
		}
	}
	for _, raw := range []string{`null`, `{}`, `[]`, `{"kind":"offer","offering_id":"` + a + `","assignment_id":"` + b + `"}`, `{"kind":"OFFER","kind":"OFFER","offering_id":"` + a + `","assignment_id":"` + b + `"}`, `{"Kind":"OFFER","offering_id":"` + a + `","assignment_id":"` + b + `"}`, `{"kind":"OFFER","offering_id":"` + a + `","assignment_id":"` + b + `","cohort_id":null}`, `{"kind":"OFFER","offering_id":"` + a + `","assignment_id":"00000000-0000-0000-0000-000000000000"}`, `{"kind":"OFFER","offering_id":"` + a + `","assignment_id":null}`} {
		var s TeacherPolicySelection
		if json.Unmarshal([]byte(raw), &s) == nil {
			t.Fatalf("ambiguous selector accepted: %s", raw)
		}
	}
}
