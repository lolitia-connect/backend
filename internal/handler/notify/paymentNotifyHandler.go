package notify

import (
	"fmt"
	"net/http"
	"time"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/notify"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/result"
)

// PaymentNotifyHandler Payment Notify
func PaymentNotifyHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		platform, ok := c.Request.Context().Value(constant.CtxKeyPlatform).(string)
		if !ok {
			logger.WithContext(c.Request.Context()).Errorf("platform not found")
			result.HttpResult(c, nil, fmt.Errorf("platform not found"))
			return
		}

		switch payment.ParsePlatform(platform) {
		case payment.EPay, payment.CryptoSaaS:
			req := &types.EPayNotifyRequest{}
			if err := c.ShouldBind(req); err != nil {
				result.HttpResult(c, nil, err)
				return
			}
			l := notify.NewEPayNotifyLogic(c, svcCtx)
			if err := l.EPayNotify(req); err != nil {
				logger.WithContext(c.Request.Context()).Errorf("EPayNotify failed: %v", err.Error())
				c.String(http.StatusBadRequest, err.Error())
				return
			}
			c.String(http.StatusOK, "%s", "success")
		case payment.Stripe:
			l := notify.NewStripeNotifyLogic(c.Request.Context(), svcCtx)
			if err := l.StripeNotify(c.Request, c.Writer); err != nil {
				result.HttpResult(c, nil, err)
				return
			}
			result.HttpResult(c, nil, nil)

		case payment.AlipayF2F:
			l := notify.NewAlipayNotifyLogic(c.Request.Context(), svcCtx)
			if err := l.AlipayNotify(c.Request); err != nil {
				result.HttpResult(c, nil, err)
				return
			}
			// Return success to alipay
			c.String(http.StatusOK, "%s", "success")

		case payment.AlipayPlus:
			l := notify.NewAlipayPlusNotifyLogic(c.Request.Context(), svcCtx)
			if err := l.AlipayPlusNotify(c.Request); err != nil {
				result.HttpResult(c, nil, err)
				return
			}
			c.Header("response-time", time.Now().Format(time.RFC3339))
			if clientID := c.GetHeader("Client-Id"); clientID != "" {
				c.Header("client-id", clientID)
			}
			c.JSON(http.StatusOK, gin.H{
				"result": gin.H{
					"resultCode":    "SUCCESS",
					"resultStatus":  "S",
					"resultMessage": "Success",
				},
			})

		default:
			logger.WithContext(c.Request.Context()).Errorf("platform %s not support", platform)
		}
	}
}
