package errors

type ErrorCode int

const (
	ErrorCodeSuccess      ErrorCode = 0 // 没有错误
	ErrorCodeUnknownError ErrorCode = 1 // 未知错误

	ErrorCodeRpcFailed          ErrorCode = 101 // rpc 调用失败
	ErrorCodeRpcTimeout         ErrorCode = 102 // rpc 调用超时
	ErrorCodeRpcServerError     ErrorCode = 103 // 依赖的接口错误
	ErrorCodeRpcUnauthorized    ErrorCode = 104 // 未授权
	ErrorCodeRpcNoQuota         ErrorCode = 105 // api 调用额度已经用完
	ErrorCodeRpcUnknownResponse ErrorCode = 106 // 位置的返回结果

	ErrorCodeBookNotFound ErrorCode = 201 // 未查询到图书

	ErrorCodeParamInvalid ErrorCode = 301 // 参数不合理
)
