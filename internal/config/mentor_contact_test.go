package config

import (
	"strings"
	"testing"
)

func TestMentorContactConfigDefaultOffAndPurposeSeparation(t *testing.T) {
	t.Setenv("DIALOG_MENTOR_CONTACT_ENABLED", "false")
	t.Setenv("DIALOG_MENTOR_INTERNAL_TOKEN", "")
	t.Setenv("DIALOG_MENTOR_CONTACT_SPACE_KEY", "")
	t.Setenv("DIALOG_CANONICAL_STUDENT_TURN_ENABLED", "false")
	cfg, err := Load()
	if err != nil || cfg.MentorContactEnabled {
		t.Fatalf("default %v", err)
	}
	valid := cfg
	valid.MentorContactEnabled = true
	valid.MentorInternalToken = strings.Repeat("m", 32)
	valid.MentorContactSpaceKey = "platform"
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing token", "short token", "space token", "teacher token", "user token", "storage token", "missing space", "invalid space"} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			switch name {
			case "missing token":
				bad.MentorInternalToken = ""
			case "short token":
				bad.MentorInternalToken = "short"
			case "space token":
				bad.MentorInternalToken += " "
			case "teacher token":
				bad.InternalAPIToken = bad.MentorInternalToken
			case "user token":
				bad.UserServiceInternalToken = bad.MentorInternalToken
			case "storage token":
				bad.FileStorageInternalToken = bad.MentorInternalToken
			case "missing space":
				bad.MentorContactSpaceKey = ""
			case "invalid space":
				bad.MentorContactSpaceKey = "../private"
			}
			if err := bad.Validate(); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}
