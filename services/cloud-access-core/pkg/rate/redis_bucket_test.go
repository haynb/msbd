package rate_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
)

func TestRedisTokenBucket(t *testing.T) {
	ctx := context.Background()
	srv := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	bucket := rate.NewRedisTokenBucket(client, "test-bucket")

	res, err := bucket.Take(ctx, "user-1", 10, 1, 5, 5*time.Second)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.InEpsilon(t, 5, res.Remaining, 0.01)

	res, err = bucket.Take(ctx, "user-1", 10, 1, 6, 5*time.Second)
	require.NoError(t, err)
	require.False(t, res.Allowed)

	srv.FastForward(6 * time.Second)

	res, err = bucket.Take(ctx, "user-1", 10, 1, 6, 5*time.Second)
	require.NoError(t, err)
	require.True(t, res.Allowed)
}
