package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func AuthMiddleware(svc *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		ctx, err := AuthenticateRequest(c.Request.Context(), svc, c.GetHeader("Authorization"), c.Request.URL.Path)
		if err != nil {
			result.HttpResult(c, nil, err)
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func AuthenticateRequest(ctx context.Context, svc *svc.ServiceContext, token string, path string) (context.Context, error) {
	jwtConfig := svc.Config.JwtAuth
	if token == "" {
		zap.S().Debug("[AuthMiddleware] Token Empty")
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.ErrorTokenEmpty), "Token Empty")
	}

	claims, err := jwt.ParseJwtToken(token, jwtConfig.AccessSecret)
	if err != nil {
		zap.S().Debug("[AuthMiddleware] ParseJwtToken", zap.Any("error", err.Error()), zap.Any("token", token))
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.ErrorTokenExpire), "Token Invalid")
	}

	loginType := ""
	if claims["LoginType"] != nil {
		loginType = claims["LoginType"].(string)
	}

	userId := int64(claims["UserId"].(float64))
	sessionId := claims["SessionId"].(string)
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	value, err := svc.Redis.Get(ctx, sessionIdCacheKey).Result()
	if err != nil {
		zap.S().Debug("[AuthMiddleware] Redis Get", zap.Any("error", err.Error()), zap.Any("sessionId", sessionId))
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	if value != fmt.Sprintf("%v", userId) {
		zap.S().Debug("[AuthMiddleware] Invalid Access", zap.Any("userId", userId), zap.Any("sessionId", sessionId))
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	userInfo, err := svc.Store.User().FindOne(ctx, userId)
	if err != nil {
		zap.S().Debug("[AuthMiddleware] UserModel FindOne", zap.Any("error", err.Error()), zap.Any("userId", userId))
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Database Query Error")
	}

	// Check if user is enabled
	if !*userInfo.Enable {
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.UserDisabled), "User Disabled")
	}

	paths := strings.Split(path, "/")
	if tool.StringSliceContains(paths, "admin") && !*userInfo.IsAdmin {
		zap.S().Debug("[AuthMiddleware] Not Admin User", zap.Any("userId", userId), zap.Any("sessionId", sessionId))
		return ctx, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	ctx = context.WithValue(ctx, constant.CtxLoginType, loginType)
	ctx = context.WithValue(ctx, constant.CtxKeyUser, userInfo)
	ctx = context.WithValue(ctx, constant.CtxKeySessionID, sessionId)
	return ctx, nil
}
