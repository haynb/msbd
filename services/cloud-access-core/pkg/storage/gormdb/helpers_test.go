package gormdb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIsUniqueViolation(t *testing.T) {
	require.True(t, IsUniqueViolation(gorm.ErrDuplicatedKey))
	require.False(t, IsUniqueViolation(errors.New("other")))
	require.False(t, IsUniqueViolation(nil))
}
