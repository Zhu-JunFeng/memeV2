package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	TraceID string      `json:"traceId"`
}

func traceID(c *gin.Context) string {
	if existing := c.GetHeader("X-Trace-Id"); existing != "" {
		return existing
	}
	return uuid.NewString()
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "成功", Data: data, TraceID: traceID(c)})
}

func Fail(c *gin.Context, status int, message string) {
	if status == 0 {
		status = http.StatusBadRequest
	}
	c.JSON(status, Body{Code: status, Message: message, Data: nil, TraceID: traceID(c)})
}
