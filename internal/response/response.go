package response

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Code    int    `json:"code" example:"0"`
	Message string `json:"message" example:"success"`
	Data    any    `json:"data"`
}

// SuccessResponse 成功响应示例结构
type SuccessResponse struct {
	Code    int    `json:"code" example:"0"`
	Message string `json:"message" example:"success"`
	Data    any    `json:"data"`
}

// ErrorResponse 失败响应结构
type ErrorResponse struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"参数错误"`
	Data    any    `json:"data"`
}

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// SuccessPublic marks a successful public GET response as short-lived cacheable.
// Keep TTL conservative so admin content updates remain visible without purge tooling.
func SuccessPublic(c *gin.Context, data any, maxAge time.Duration) {
	if maxAge <= 0 {
		maxAge = 30 * time.Second
	}
	seconds := int(maxAge.Seconds())
	c.Header(
		"Cache-Control",
		"public, max-age="+strconv.Itoa(seconds)+", stale-while-revalidate="+strconv.Itoa(seconds*2),
	)
	Success(c, data)
}
