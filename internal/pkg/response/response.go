package response

import "github.com/gin-gonic/gin"

type Body struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func OK(c *gin.Context, data interface{}, message string) {
	c.JSON(200, Body{Success: true, Data: data, Message: message})
}

func Created(c *gin.Context, data interface{}, message string) {
	c.JSON(201, Body{Success: true, Data: data, Message: message})
}

func Accepted(c *gin.Context, data interface{}, message string) {
	c.JSON(202, Body{Success: true, Data: data, Message: message})
}

func WithMeta(c *gin.Context, status int, data interface{}, meta interface{}, message string) {
	c.JSON(status, Body{Success: true, Data: data, Meta: meta, Message: message})
}

func Error(c *gin.Context, status int, code string, message string) {
	c.JSON(status, Body{Success: false, Error: &ErrorBody{Code: code, Message: message}})
}
