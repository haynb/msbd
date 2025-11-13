package gormdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

var (
	rootTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	rootUserID   = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
)

// EnsureSeedData inserts the root tenant and admin user if missing.
func EnsureSeedData(ctx context.Context, repo *Repository) error {
	return repo.WithTransaction(ctx, func(tx *gorm.DB) error {
		txRepo := NewRepository(tx)

		err := txRepo.EnsureTenantExists(ctx, rootTenantID)
		if errors.Is(err, ErrNotFound) {
			if err := txRepo.CreateTenant(ctx, &Tenant{
				BaseModel: BaseModel{ID: rootTenantID},
				Name:      "root",
				Tier:      "enterprise",
			}); err != nil {
				return fmt.Errorf("create root tenant: %w", err)
			}
		} else if err != nil {
			return err
		}

		if _, err := txRepo.UserByEmail(ctx, "admin@cloud-access.local"); errors.Is(err, ErrNotFound) {
			admin := &User{
				BaseModel:    BaseModel{ID: rootUserID},
				TenantID:     rootTenantID,
				Email:        "admin@cloud-access.local",
				PasswordHash: []byte("$argon2id$v=19$m=65536,t=1,p=10$TkQfjvPPNFcQINLRYVLqAg$tXYn8ijlVgtw+PzxzfjAtU0zrFn0d4Eu5Qa+il+Mk9Y"),
				Roles:        pq.StringArray{"admin"},
			}
			if err := txRepo.CreateUser(ctx, admin); err != nil {
				return fmt.Errorf("create root admin: %w", err)
			}
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		return nil
	})
}
