package response

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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

func Error(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Envelope{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}
