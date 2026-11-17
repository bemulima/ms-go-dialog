package main

import (
	"crypto/x509"
	"github.com/bemulima/ms-go-dialog/internal/config"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	mentorclient "github.com/bemulima/ms-go-dialog/internal/infrastructure/http/mentor"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"os"
)

func newMentorEducation(cfg config.Config, dialogs *dialoguc.Service, bindings repository.MentorRepository) (*dialoguc.MentorEducationService, error) {
	service := &dialoguc.MentorEducationService{Enabled: cfg.MentorEducationEnabled, SpaceKey: cfg.MentorEducationSpaceKey, Dialogs: dialogs, Bindings: bindings}
	if !cfg.MentorEducationEnabled {
		return service, nil
	}
	var roots *x509.CertPool
	if cfg.MentorAccessCAFile != "" {
		raw, err := os.ReadFile(cfg.MentorAccessCAFile)
		if err != nil {
			return nil, domain.ErrDependencyUnavailable
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(raw) {
			return nil, domain.ErrDependencyUnavailable
		}
	}
	owner, err := mentorclient.New(mentorclient.Config{Origin: cfg.MentorAccessOrigin, Token: cfg.MentorAccessToken, RootCAs: roots})
	if err != nil {
		return nil, err
	}
	service.Owner = owner
	return service, nil
}
