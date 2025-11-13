package unit

import (
	"context"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

type noopAuditor struct{}

func (noopAuditor) Record(context.Context, audit.Entry) {}

type stubUserRepo struct {
	user *gormdb.User
}

func (s *stubUserRepo) UserByEmail(_ context.Context, email string) (*gormdb.User, error) {
	if s.user != nil && email == s.user.Email {
		c := *s.user
		return &c, nil
	}
	return nil, gormdb.ErrNotFound
}

func (s *stubUserRepo) UserByID(_ context.Context, id uuid.UUID) (*gormdb.User, error) {
	if s.user != nil && id == s.user.ID {
		c := *s.user
		return &c, nil
	}
	return nil, gormdb.ErrNotFound
}
