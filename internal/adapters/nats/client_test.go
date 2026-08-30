package nats

import "testing"

func TestMergeSubjects_AddsRequiredWithoutRemovingOwnedFutureSubjects(t *testing.T) {
	actual := mergeSubjects([]string{"dialog.created", "dialog.future"}, []string{"dialog.created", "dialog.teacher.requested"})
	expected := []string{"dialog.created", "dialog.future", "dialog.teacher.requested"}
	if !sameSubjects(actual, expected) {
		t.Fatalf("subjects=%v want=%v", actual, expected)
	}
}
