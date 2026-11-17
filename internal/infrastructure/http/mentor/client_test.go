package mentor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"testing"
)

type educationRoundTrip func(*http.Request) (*http.Response, error)

func (f educationRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMentorEducationOwnerClientExactBoundedWire(t *testing.T) {
	actor, mentor, source, membership := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	token := strings.Repeat("d", 40)
	valid := map[string]any{"schema": "mentor.dialog-access.v1", "kind": "ASSIGNMENT", "id": source, "actor_id": actor, "mentor_id": mentor, "student_id": actor, "revision": 3, "membership_id": membership, "history_from_revision": 3, "authors": []any{map[string]any{"user_id": actor, "membership_id": membership}}}
	raw, _ := json.Marshal(valid)
	for _, test := range []struct {
		name, body string
		status     int
		headers    func(http.Header)
		deny       bool
	}{
		{"valid", string(raw), 200, nil, false}, {"duplicate", strings.Replace(string(raw), `"revision":3`, `"revision":3,"revision":3`, 1), 200, nil, true}, {"wrong actor", strings.Replace(string(raw), actor.String(), uuid.New().String(), 1), 200, nil, true},
		{"null authors", strings.Replace(string(raw), `"authors":[{"membership_id":"`+membership.String()+`","user_id":"`+actor.String()+`"}]`, `"authors":null`, 1), 200, nil, true},
		{"fraction", strings.Replace(string(raw), `"revision":3`, `"revision":3.0`, 1), 200, nil, true}, {"large", strings.Repeat(" ", 8193), 200, nil, true},
		{"cache duplicate", string(raw), 200, func(h http.Header) { h.Add("Cache-Control", "no-store") }, true},
		{"private denial", "", 403, nil, true}, {"redirect", "", 302, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, err := New(Config{Origin: "https://mentor.invalid", Token: token})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			c.http.Transport = educationRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://mentor.invalid"+AccessPath || r.Method != "POST" || r.Header.Get("X-Mentor-Dialog-Token") != token || r.Header.Get("Authorization") != "" {
					t.Fatal("wrong purpose transport")
				}
				var request map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 5 || string(request["author_ids"]) == "null" {
					t.Fatal("wrong request")
				}
				h := http.Header{"Content-Type": []string{"application/json"}, "Cache-Control": []string{"no-store"}}
				if test.headers != nil {
					test.headers(h)
				}
				return &http.Response{StatusCode: test.status, Header: h, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			proof, err := c.ReadAccess(context.Background(), domain.MentorAssignment, source, actor, []uuid.UUID{actor})
			if test.deny && err == nil {
				t.Fatalf("invalid owner accepted: %+v", proof)
			}
			if !test.deny && (err != nil || proof.Authors[actor] != membership) {
				t.Fatalf("valid owner=%+v %v", proof, err)
			}
			if calls != 1 {
				t.Fatal("retry/fallback")
			}
			if test.status == 403 && !errors.Is(err, domain.ErrForbidden) {
				t.Fatal(err)
			}
		})
	}
	for _, origin := range []string{"http://mentor.invalid", "https://mentor.invalid/path", "https://user@mentor.invalid", "https://mentor.invalid?x=1"} {
		if _, err := New(Config{Origin: origin, Token: token}); err == nil {
			t.Fatal("unsafe origin", origin)
		}
	}
}
