package ginfmt

// BaseResponse 基本 Response 结构
type BaseResponse struct {
	// 状态码，请求成功为 0，请求失败为具体错误码
	Code int `json:"code"`
	// 错误信息，会配置具体错误信息，但不一定可以用于前端直接展示
	Message string `json:"message"`
	// 返回数据
	Data interface{} `json:"data"`
}

func NewBaseResponse(data interface{}) *BaseResponse {
	return &BaseResponse{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

func NewErrorResponse(code int, message string) *BaseResponse {
	return &BaseResponse{
		Code:    code,
		Message: message,
	}
}
