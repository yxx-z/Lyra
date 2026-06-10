package subsonic

import (
	"context"

	"github.com/yxx-z/lyra/internal/auth"
)

type ctxKey int

const userCtxKey ctxKey = iota

func withUser(ctx context.Context, u *auth.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

func userFromCtx(ctx context.Context) *auth.User {
	u, _ := ctx.Value(userCtxKey).(*auth.User)
	return u
}
