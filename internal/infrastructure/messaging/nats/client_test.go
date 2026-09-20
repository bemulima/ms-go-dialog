package nats

import "testing"

func TestDurableSubjectsIncludeTeacherContextMutation(t *testing.T) {
	for _, subject := range durableSubjects {
		if subject == "dialog.teacher.context-mutated" {
			return
		}
	}
	t.Fatal("teacher context mutation subject is not retained by DIALOG_EVENTS")
}

func TestMergeSubjects_AddsRequiredWithoutRemovingOwnedFutureSubjects(t *testing.T) {
	actual := mergeSubjects([]string{"dialog.created", "dialog.future"}, []string{"dialog.created", "dialog.teacher.requested"})
	expected := []string{"dialog.created", "dialog.future", "dialog.teacher.requested"}
	if !sameSubjects(actual, expected) {
		t.Fatalf("subjects=%v want=%v", actual, expected)
	}
}
