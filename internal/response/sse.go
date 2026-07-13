package response

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

func PrepareSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
}

func WriteSSE(c *gin.Context, event string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}
