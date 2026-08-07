package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	HardMaxGroupMembers = 1000
	HardMaxBodyLength   = 100000
	HardMaxAttachments  = 20
	HardMaxImageBytes   = int64(25 << 20)
	HardMaxFileBytes    = int64(100 << 20)
)

var spaceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

type SpaceStatus int16

const (
	SpaceStatusDisabled SpaceStatus = iota
	SpaceStatusActive
)

type Policy struct {
	AllowPersonal        bool
	AllowGroups          bool
	AllowImages          bool
	AllowFiles           bool
	AllowLinks           bool
	MaxGroupMembers      int
	MaxBodyLength        int
	MaxAttachments       int16
	MaxImageBytes        int64
	MaxFileBytes         int64
	AllowedFileMIMETypes []string
	EditWindowSeconds    int
}

func DefaultPolicy() Policy {
	return Policy{
		AllowPersonal: true, AllowGroups: true, AllowImages: true, AllowFiles: true, AllowLinks: true,
		MaxGroupMembers: 500, MaxBodyLength: 10000, MaxAttachments: 10,
		MaxImageBytes: 25 << 20, MaxFileBytes: 100 << 20, EditWindowSeconds: 900,
		AllowedFileMIMETypes: []string{"application/pdf", "text/plain", "text/csv"},
	}
}

func (p Policy) Validate() error {
	if !p.AllowPersonal && !p.AllowGroups {
		return fmt.Errorf("%w: at least one dialog type must be enabled", ErrValidation)
	}
	if p.MaxGroupMembers < 2 || p.MaxGroupMembers > HardMaxGroupMembers ||
		p.MaxBodyLength < 1 || p.MaxBodyLength > HardMaxBodyLength ||
		p.MaxAttachments < 0 || p.MaxAttachments > HardMaxAttachments ||
		p.MaxImageBytes < 1 || p.MaxImageBytes > HardMaxImageBytes ||
		p.MaxFileBytes < 1 || p.MaxFileBytes > HardMaxFileBytes ||
		p.EditWindowSeconds < 0 || p.EditWindowSeconds > 7*24*60*60 {
		return fmt.Errorf("%w: policy limit is outside hard bounds", ErrValidation)
	}
	for _, mime := range p.AllowedFileMIMETypes {
		if strings.TrimSpace(mime) == "" || !strings.Contains(mime, "/") {
			return fmt.Errorf("%w: invalid allowed file MIME type", ErrValidation)
		}
	}
	return nil
}

func (p Policy) AllowsFileMIME(mime string) bool {
	for _, allowed := range p.AllowedFileMIMETypes {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(mime)) {
			return true
		}
	}
	return false
}

type Space struct {
	ID             uuid.UUID
	Key            string
	Name           string
	Status         SpaceStatus
	AllowedOrigins []string
	Policy         Policy
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s Space) Validate() error {
	if s.ID == uuid.Nil || s.CreatedBy == uuid.Nil || !spaceKeyPattern.MatchString(s.Key) || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: invalid space identity", ErrValidation)
	}
	if s.Status != SpaceStatusDisabled && s.Status != SpaceStatusActive {
		return fmt.Errorf("%w: invalid space status", ErrValidation)
	}
	if err := s.Policy.Validate(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(s.AllowedOrigins))
	for _, origin := range s.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%w: allowed origins must be exact scheme and host values", ErrValidation)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("%w: unsupported origin scheme", ErrValidation)
		}
		if _, ok := seen[origin]; ok {
			return fmt.Errorf("%w: duplicate origin", ErrValidation)
		}
		seen[origin] = struct{}{}
	}
	return nil
}
