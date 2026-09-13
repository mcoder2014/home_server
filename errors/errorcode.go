package errors

type ErrorCode int

const (
	// 通用的
	ErrorCodeSuccess       ErrorCode = 0  // 没有错误
	ErrorCodeUnknownError  ErrorCode = 1  // 未知错误
	ErrorCodeParamInvalid  ErrorCode = 2  // 参数不合理
	ErrorCodeForbidden     ErrorCode = 3  // 已认证，但没有操作权限
	ErrorCodeNotFound      ErrorCode = 4  // 资源不存在或不可见
	ErrorCodeConflict      ErrorCode = 5  // 并发修订或幂等冲突
	ErrorCodeTooLarge      ErrorCode = 6  // 请求或产物超过限制
	ErrorCodeUnsupported   ErrorCode = 7  // 不支持的媒体或操作类型
	ErrorCodeRateLimited   ErrorCode = 8  // 频率或容量限制
	ErrorCodeUnprocessable ErrorCode = 9  // 请求格式正确，但内容无效
	ErrorCodeDependency    ErrorCode = 10 // 数据库、存储或其他依赖不可用

	// 远程调用相关的
	ErrorCodeRpcFailed          ErrorCode = 101 // rpc 调用失败
	ErrorCodeRpcTimeout         ErrorCode = 102 // rpc 调用超时
	ErrorCodeRpcServerError     ErrorCode = 103 // 依赖的接口错误
	ErrorCodeRpcUnauthorized    ErrorCode = 104 // 未授权
	ErrorCodeRpcNoQuota         ErrorCode = 105 // api 调用额度已经用完
	ErrorCodeRpcUnknownResponse ErrorCode = 106 // 位置的返回结果

	// 业务相关的
	ErrorCodeBookNotFound    ErrorCode = 201 // 未查询到图书
	ErrorCodeStorageNotFount ErrorCode = 202 // 未查询到 Storage
	ErrorCodePreCheckFailed  ErrorCode = 203 // 预检查步骤出错
	ErrorCodeStorageHasExist ErrorCode = 204 // 库存记录已经存在

	// 数据库相关的
	ErrorCodeDbError ErrorCode = 301 // 数据库错误

	// 登录相关
	ErrorCodeUserNameOrPasswdWrong = 401 // 用户名密码不正确
	ErrorCodeGenRsaKeyFailed       = 402 // RSA 秘钥生成错误
	ErrorCodeUserNotLogin          = 403 // 用户未登录
	ErrorCodeUserLoginExpire       = 404 // 用户登录态过期
)
