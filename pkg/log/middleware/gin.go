package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"moff.io/moff-social/pkg/log/meta"
	"net/http"
	"strings"
	"time"
)

// ///////////////////////////////////////////////////////////
// ///////////////////   Gin Middleware  /////////////////////
// ///////////////////////////////////////////////////////////
// Custom response writer to record handler response body.
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write writes response message into response body and the connection.
func (r responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

type httpInfo struct {
	Headers       map[string]string `json:"headers"`
	Method        string            `json:"method"`
	RequestAPI    string            `json:"request_api,omitempty"`
	RemoteAddr    string            `json:"remote_addr,omitempty"`
	Response      *response         `json:"response,omitempty"`
	ExecutionTime string            `json:"execution_time,omitempty"`
}

func newHTTPInfo(ctx *gin.Context) *httpInfo {
	return &httpInfo{
		Headers:    requestHeaderFilter(ctx.Request.Header),
		Method:     ctx.Request.Method,
		RequestAPI: ctx.Request.RequestURI,
		RemoteAddr: ctx.ClientIP(),
	}
}

// RecoveredHTTPLog gin框架请求日志拦截器，拦截请求与响应，打印日志
// 注意：如果启用分布式追踪，需要在分布式追踪中间件后注册该拦截器
func RecoveredHTTPLog() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 启用日志元信息
		rctx := meta.Begin(ctx.Request.Context())
		ctx.Request = ctx.Request.WithContext(rctx)

		// 自定义writer，抓取响应
		w := &responseBodyWriter{body: &bytes.Buffer{}, ResponseWriter: ctx.Writer}
		ctx.Writer = w

		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				err := errors.ErrorfAndReport("%v", r)
				log.Error(err)
			}
			logHTTP(ctx, w, start)
		}()
		ctx.Next()
	}
}

const defaultRequestTimeout = time.Second * 60

// TimeoutHTTP HTTP超时拦截器
func TimeoutHTTP(timeout ...time.Duration) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		timeoutCtx, cancelFunc := context.WithTimeout(ctx.Request.Context(), defaultRequestTimeout)
		if len(timeout) != 0 && timeout[0] > 0 {
			timeoutCtx, cancelFunc = context.WithTimeout(ctx.Request.Context(), timeout[0])
		}
		defer cancelFunc()
		ctx.Request = ctx.Request.WithContext(timeoutCtx)
		ctx.Next()
	}
}

// 根据响应状态，打印http日志
func logHTTP(ctx *gin.Context, w *responseBodyWriter, start time.Time) {
	// 如果没有写入响应则写入内部错误
	if !ctx.Writer.Written() {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"code": 5000,
			"msg":  "Server internal error",
		})
	}

	s := w.Status()
	info := newHTTPInfo(ctx)
	info.Response = decodeHandlerResponse(w.body.Bytes(), s)
	info.ExecutionTime = fmt.Sprintf("%vms", time.Since(start).Nanoseconds()/1e6)
	switch s {
	case http.StatusOK:
		log.Info(info)
	case http.StatusInternalServerError:
		log.Error(info)
	default:
		log.Warn(info)
	}
	ctx.Header("request-id", ctx.Request.Header.Get("x-request-id"))
}

type response struct {
	//ProtocolCode is the response protocol status code
	ProtocolCode int `json:"protocol_code"`
	//Code is the response business code.
	Code interface{} `json:"code,omitempty"`
	//Message is the response message.
	Message interface{} `json:"msg,omitempty"`
}

func decodeHandlerResponse(respBody []byte, httpCode int) *response {
	var resp response
	resp.ProtocolCode = httpCode
	if err := json.Unmarshal(respBody, &resp); err == nil {
	}
	return &resp
}

var excludedHeaders = map[string]bool{
	"token":        true,
	"access-token": true,
}

func requestHeaderFilter(headers map[string][]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range headers {
		k = strings.ToLower(k)
		if excludedHeaders[k] {
			continue
		}
		filtered[k] = strings.Join(v, ";")
	}
	return filtered
}
