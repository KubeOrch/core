package api

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const RequestIDKey = "requestID"

type FieldViolation struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Problem struct {
	Type      string           `json:"type"`
	Title     string           `json:"title"`
	Status    int              `json:"status"`
	Detail    string           `json:"detail,omitempty"`
	Instance  string           `json:"instance,omitempty"`
	Code      string           `json:"code"`
	RequestID string           `json:"requestId"`
	Errors    []FieldViolation `json:"errors,omitempty"`
}

func RequestID(c *gin.Context) string {
	if value, ok := c.Get(RequestIDKey); ok {
		if requestID, valid := value.(string); valid && requestID != "" {
			return requestID
		}
	}

	requestID := uuid.NewString()
	c.Set(RequestIDKey, requestID)
	c.Header("X-Request-Id", requestID)
	return requestID
}

func WriteProblem(c *gin.Context, status int, code, title, detail string, violations ...FieldViolation) {
	requestID := RequestID(c)
	c.Header("Content-Type", "application/problem+json")
	c.Header("X-Request-Id", requestID)
	c.JSON(status, Problem{
		Type:      fmt.Sprintf("https://kubeorch.dev/problems/%s", code),
		Title:     title,
		Status:    status,
		Detail:    detail,
		Instance:  c.Request.URL.Path,
		Code:      code,
		RequestID: requestID,
		Errors:    violations,
	})
}

func AbortProblem(c *gin.Context, status int, code, title, detail string) {
	WriteProblem(c, status, code, title, detail)
	c.Abort()
}
