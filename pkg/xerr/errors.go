package xerr

import (
	"errors"
	"fmt"
)

/**
General common fixed error
*/

type CodeError struct {
	errCode uint32
	errMsg  string
}

var StatusNotModified = errors.New("304 Not Modified")

// GetErrCode returns the error code displayed to the front end
func (e *CodeError) GetErrCode() uint32 {
	return e.errCode
}

// GetErrMsg returns the error message displayed to the front end
func (e *CodeError) GetErrMsg() string {
	return e.errMsg
}

func (e *CodeError) Error() string {
	return fmt.Sprintf("ErrCode:%d，ErrMsg:%s", e.errCode, e.errMsg)
}

func NewErrCodeMsg(errCode uint32, errMsg string) *CodeError {
	return &CodeError{errCode: errCode, errMsg: errMsg}
}
func NewErrCode(errCode uint32) *CodeError {
	return &CodeError{errCode: errCode, errMsg: MapErrMsg(errCode)}
}

// IsRejection identifies a refused request even when the API returns HTTP 200.
// Unknown codes and infrastructure errors are deliberately left visible.
func IsRejection(err error) bool {
	var e *CodeError
	if !errors.As(err, &e) {
		return false
	}
	switch e.errCode {
	case InvalidParams, TooManyRequests, ErrorTokenEmpty, ErrorTokenInvalid, ErrorTokenExpire, InvalidAccess,
		InvalidCiphertext, PermissionDenied, UserExist, UserNotExist, UserPasswordError, UserDisabled,
		InsufficientBalance, StopRegister, TelegramNotBound, UserNotBindOauth, InviteCodeError, UserCommissionNotEnough, RegisterIPLimit,
		NodeExist, NodeNotExist, NodeGroupExist, NodeGroupNotExist, NodeGroupNotEmpty,
		CouponNotExist, CouponAlreadyUsed, CouponNotApplicable, CouponInsufficientUsage, CouponExpired, CouponDisabled,
		SubscribeExpired, SubscribeNotAvailable, UserSubscribeExist, SubscribeIsUsedError, SingleSubscribeModeExceedsLimit,
		SubscribeQuotaLimit, SubscribeOutOfStock, TrafficExhausted, VerifyCodeError, SmsNotEnabled, EmailNotEnabled,
		AuthenticatorNotSupportedError, TelephoneAreaCodeIsEmpty, TodaySendCountExceedsLimit, PasswordIsEmpty, AreaCodeIsEmpty,
		PasswordOrVerificationCodeRequired, EmailExist, TelephoneExist, DeviceExist, TelephoneError, DeviceNotExist, UseridNotMatch,
		OrderNotExist, PaymentMethodNotFound, OrderStatusError, InsufficientOfPeriod, ExistAvailableTraffic, WithdrawalAlreadyReviewed:
		return true
	}
	return false
}

func NewErrMsg(errMsg string) *CodeError {
	return &CodeError{errCode: ERROR, errMsg: errMsg}
}
