package response

import "github.com/gin-gonic/gin"

func Error(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Envelope{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}

func ErrorWithData(c *gin.Context, status int, message string, data any) {
	c.JSON(status, Envelope{
		Code:    status,
		Message: message,
		Data:    data,
	})
}
