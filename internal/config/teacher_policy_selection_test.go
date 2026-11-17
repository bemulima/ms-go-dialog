package config

import "testing"

func TestTeacherPolicySelectionConfigDefaultOff(t *testing.T) {
	t.Setenv("DIALOG_TEACHER_MENTOR_POLICY_ENABLED", "false")
	c, e := Load()
	if e != nil || c.TeacherMentorPolicyEnabled {
		t.Fatalf("new source admission not dark %v", e)
	}
	t.Setenv("DIALOG_TEACHER_MENTOR_POLICY_ENABLED", "true")
	c, e = Load()
	if e != nil || !c.TeacherMentorPolicyEnabled {
		t.Fatalf("explicit source admission not parsed %v", e)
	}
}
