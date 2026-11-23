package mentor

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/google/uuid"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const AccessPath = "/internal/dialog/v1/context-access"

type Config struct {
	Origin, Token string
	RootCAs       *x509.CertPool
}
type Client struct {
	origin, token string
	http          *http.Client
}

func New(c Config) (*Client, error) {
	u, err := url.Parse(c.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || len(c.Token) < 32 || len(c.Token) > 256 {
		return nil, domain.ErrDependencyUnavailable
	}
	for _, ch := range c.Token {
		if ch < 33 || ch > 126 || ch == ',' {
			return nil, domain.ErrDependencyUnavailable
		}
	}
	transport := &http.Transport{Proxy: nil, DisableCompression: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.RootCAs}, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 8192}
	return &Client{origin: strings.TrimSuffix(u.String(), "/"), token: c.Token, http: &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) ReadAccess(ctx context.Context, kind domain.MentorContextKind, id, actor uuid.UUID, authors []uuid.UUID) (dialoguc.MentorOwnerProof, error) {
	var zero dialoguc.MentorOwnerProof
	if c == nil || c.http == nil || !kind.Valid() || id == uuid.Nil || actor == uuid.Nil || len(authors) > 32 {
		return zero, domain.ErrDependencyUnavailable
	}
	ids := []string{}
	seen := map[uuid.UUID]bool{}
	for _, author := range authors {
		if author == uuid.Nil || seen[author] {
			return zero, domain.ErrValidation
		}
		seen[author] = true
		ids = append(ids, author.String())
	}
	sort.Strings(ids)
	body, _ := json.Marshal(struct {
		Schema    string                   `json:"schema"`
		Kind      domain.MentorContextKind `json:"kind"`
		ID        uuid.UUID                `json:"id"`
		ActorID   uuid.UUID                `json:"actor_id"`
		AuthorIDs []string                 `json:"author_ids"`
	}{"mentor.dialog-access-request.v1", kind, id, actor, ids})
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", c.origin+AccessPath, bytes.NewReader(body))
	if err != nil {
		return zero, domain.ErrDependencyUnavailable
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cache-Control", "no-store")
	r.Header.Set("X-Mentor-Dialog-Token", c.token)
	res, err := c.http.Do(r)
	if err != nil {
		return zero, domain.ErrDependencyUnavailable
	}
	defer res.Body.Close()
	stop := context.AfterFunc(ctx, func() { _ = res.Body.Close() })
	defer stop()
	if res.StatusCode == 403 || res.StatusCode == 404 {
		return zero, domain.ErrForbidden
	}
	if res.StatusCode != 200 || len(res.Header.Values("Cache-Control")) != 1 || res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Content-Encoding") != "" || res.Header.Get("Content-Type") != "application/json" || res.ContentLength > 8192 {
		return zero, domain.ErrDependencyUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if err != nil || len(raw) > 8192 || ctx.Err() != nil {
		return zero, domain.ErrDependencyUnavailable
	}
	keys := []string{"schema", "kind", "id", "actor_id", "mentor_id", "student_id", "revision", "membership_id", "history_from_revision", "authors"}
	root, err := strict(raw, keys)
	if err != nil {
		return zero, domain.ErrDependencyUnavailable
	}
	var w struct {
		Schema                                         string                   `json:"schema"`
		Kind                                           domain.MentorContextKind `json:"kind"`
		ID, ActorID, MentorID, StudentID, MembershipID string
		Revision, Floor                                int64
		Authors                                        []json.RawMessage
	}
	if json.Unmarshal(root["schema"], &w.Schema) != nil || json.Unmarshal(root["kind"], &w.Kind) != nil || json.Unmarshal(root["id"], &w.ID) != nil || json.Unmarshal(root["actor_id"], &w.ActorID) != nil || json.Unmarshal(root["mentor_id"], &w.MentorID) != nil || json.Unmarshal(root["student_id"], &w.StudentID) != nil || json.Unmarshal(root["membership_id"], &w.MembershipID) != nil || json.Unmarshal(root["revision"], &w.Revision) != nil || json.Unmarshal(root["history_from_revision"], &w.Floor) != nil || json.Unmarshal(root["authors"], &w.Authors) != nil {
		return zero, domain.ErrDependencyUnavailable
	}
	canonicalID := func(text string) uuid.UUID {
		v, e := uuid.Parse(text)
		if e != nil || v == uuid.Nil || v.String() != text {
			return uuid.Nil
		}
		return v
	}
	p := dialoguc.MentorOwnerProof{Kind: w.Kind, SourceID: canonicalID(w.ID), ActorID: canonicalID(w.ActorID), MentorID: canonicalID(w.MentorID), MembershipID: canonicalID(w.MembershipID), Revision: w.Revision, HistoryFromRevision: w.Floor, Authors: map[uuid.UUID]uuid.UUID{}}
	if w.StudentID != "" {
		p.StudentID = canonicalID(w.StudentID)
		if p.StudentID == uuid.Nil {
			return zero, domain.ErrDependencyUnavailable
		}
	}
	if w.Schema != "mentor.dialog-access.v1" || p.Kind != kind || p.SourceID != id || p.ActorID != actor || p.MentorID == uuid.Nil || p.MembershipID == uuid.Nil || p.Revision < 1 || p.Revision > 9007199254740991 || p.HistoryFromRevision < 1 || p.HistoryFromRevision > p.Revision || (kind == domain.MentorAssignment && (p.StudentID == uuid.Nil || p.StudentID == p.MentorID)) || (kind == domain.MentorCohort && w.StudentID != "") || len(w.Authors) > len(ids) {
		return zero, domain.ErrDependencyUnavailable
	}
	for _, rawAuthor := range w.Authors {
		fields, e := strict(rawAuthor, []string{"user_id", "membership_id"})
		if e != nil {
			return zero, domain.ErrDependencyUnavailable
		}
		var user, member string
		if json.Unmarshal(fields["user_id"], &user) != nil || json.Unmarshal(fields["membership_id"], &member) != nil {
			return zero, domain.ErrDependencyUnavailable
		}
		uid, mid := canonicalID(user), canonicalID(member)
		if uid == uuid.Nil || mid == uuid.Nil || !seen[uid] || p.Authors[uid] != uuid.Nil {
			return zero, domain.ErrDependencyUnavailable
		}
		p.Authors[uid] = mid
	}
	return p, nil
}
func strict(raw []byte, keys []string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, domain.ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	opening, err := d.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, domain.ErrValidation
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || out[key] != nil {
			return nil, domain.ErrValidation
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, domain.ErrValidation
		}
		allowed := false
		for _, expected := range keys {
			if expected == key {
				allowed = true
			}
		}
		if !allowed {
			return nil, domain.ErrValidation
		}
		out[key] = value
	}
	if closing, err := d.Token(); err != nil || closing != json.Delim('}') || d.Decode(&struct{}{}) != io.EOF || len(out) != len(keys) {
		return nil, domain.ErrValidation
	}
	return out, nil
}
