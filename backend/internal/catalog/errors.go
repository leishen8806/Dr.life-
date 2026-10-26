package catalog

import "errors"

// 领域错误：接口层分别映射为 1003 not_found 与 2002 permission_denied。
var (
	ErrNotFound  = errors.New("catalog: resource not found")
	ErrForbidden = errors.New("catalog: merchant cannot modify another merchant product")
	// ErrMerchantUnknown 表示 MerchantDirectory 里没有这个商户（未注册或已删除）。
	ErrMerchantUnknown = errors.New("catalog: merchant is unknown to the directory")
)

// ValidationError 是服务端字段级校验失败，接口层映射为 1002 invalid_argument。
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "catalog: invalid " + e.Field + ": " + e.Reason
}
