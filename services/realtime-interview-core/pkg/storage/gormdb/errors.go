package gormdb

import (
	"errors"

	"gorm.io/gorm"
)

// IsUniqueViolation reports whether the error originated from a unique constraint.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
