package middleware

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"moff.io/moff-social/pkg/log/meta"
	"net"
	"time"
)

// ///////////////////////////////////////////////////////////
// /////////////////// grpc interceptor  /////////////////////
// ///////////////////////////////////////////////////////////
const (
	defaultXRequestIDHeader      = "x-request-id"
	defaultXB3TraceIDHeader      = "x-b3-traceid"
	defaultXB3SpanIDHeader       = "x-b3-spanid"
	defaultXB3ParentSpanIDHeader = "x-b3-parentspanid"
	defaultXB3SampledHeader      = "x-b3-sampled"
	defaultXB3FlagsHeader        = "x-b3-flags"
	defaultXOTSpanContextHeader  = "x-ot-span-context"

	loopIDHeader         = "x-loop-id"
	loopUserIDHeader     = "x-loop-userid"
	loopUsernameHeader   = "x-loop-username"
	loopCanaryModeHeader = "canary-mode"
)

// UnaryClientTraceInterceptor 提取追踪信息注入请求上下文
func UnaryClientTraceInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = newOutgoingTraceContext(ctx)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// StreamClientTraceInterceptor 提取追踪信息注入请求上下文
func StreamClientTraceInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = newOutgoingTraceContext(ctx)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

func newOutgoingTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		// 拷贝MD以保证修改安全
		md = md.Copy()
	} else {
		md = metadata.MD{}
	}
	traceIsitoRequest(ctx, &md)
	traceUserRequest(ctx, &md)
	traceRequestMode(ctx, &md)
	return metadata.NewOutgoingContext(ctx, md)
}

func traceIsitoRequest(ctx context.Context, outgoing *metadata.MD) {
	injectKeyIntoOutgoingMetadata(ctx, outgoing,
		defaultXRequestIDHeader,
		defaultXB3TraceIDHeader, defaultXB3SpanIDHeader, defaultXB3ParentSpanIDHeader,
		defaultXB3SampledHeader, defaultXB3FlagsHeader,
		defaultXOTSpanContextHeader)
}

func traceUserRequest(ctx context.Context, outgoing *metadata.MD) {
	injectKeyIntoOutgoingMetadata(ctx, outgoing,
		loopIDHeader, loopUserIDHeader, loopUsernameHeader)
}

func traceRequestMode(ctx context.Context, outgoing *metadata.MD) {
	injectKeyIntoOutgoingMetadata(ctx, outgoing, loopCanaryModeHeader)
}

func injectKeyIntoOutgoingMetadata(ctx context.Context, outgoing *metadata.MD, keys ...string) {
	for _, key := range keys {
		val, _ := meta.Value(ctx, key).(string)
		if val != "" {
			outgoing.Set(key, val)
		}
	}
}

type gRPCInfo struct {
	Headers       map[string]string `json:"headers"`
	RequestAPI    string            `json:"request_api,omitempty"`
	RemoteAddr    string            `json:"remote_addr,omitempty"`
	Parameter     interface{}       `json:"parameter,omitempty"`
	Response      *response         `json:"response,omitempty"`
	ExecutionTime string            `json:"execution_time,omitempty"`
}

// RecoveredUnaryGRPCServerLog 单点gRPC请求日志拦截器，拦截请求与响应，打印日志信息
func RecoveredUnaryGRPCServerLog(options ...InterceptorOption) grpc.UnaryServerInterceptor {
	config := defaultGRPCConfig()
	for _, optFunc := range options {
		optFunc(config)
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		// 上下文开启日志元信息
		ctx = meta.Begin(ctx)
		headers, _ := metadata.FromIncomingContext(ctx)
		start := time.Now()
		defer func() {
			// handler异常panic
			if r := recover(); r != nil {
				err = errors.ErrorfAndReport("%v", r)
			}
			params := req
			// 是否记录方法参数
			if config.noLogUnaryRequestParamMethods[info.FullMethod] {
				params = nil
			}
			if e := logGRPC(ctx, params, headers, info.FullMethod, start, err); e != nil {
				// 替换响应的错误为内部错误
				err = status.Error(codes.Internal, "系统错误，联系管理员")
			}
		}()
		// 执行handler
		resp, err = handler(ctx, req)
		return resp, err
	}
}

// TimeoutUnaryGRPC 超时单点grpc调用
func TimeoutUnaryGRPC(timeout ...time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		timeoutCtx, cancelFunc := context.WithTimeout(ctx, defaultRequestTimeout)
		if len(timeout) != 0 && timeout[0] > 0 {
			timeoutCtx, cancelFunc = context.WithTimeout(ctx, timeout[0])
		}
		defer cancelFunc()
		return handler(timeoutCtx, req)
	}
}

// RecoveredStreamServerLog 流式gRPC请求日志拦截器，拦截请求与响应，打印日志信息
func RecoveredStreamServerLog() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx := ss.Context()
		// 上下文开启日志元信息
		ctx = meta.Begin(ctx)
		headers, _ := metadata.FromIncomingContext(ctx)
		start := time.Now()
		defer func() {
			// handler异常panic并且打印错误日志
			if r := recover(); r != nil {
				err = errors.ErrorfAndReport("%v", r)
				log.Error(err)
			}
			if e := logGRPC(ctx, nil, headers, info.FullMethod, start, err); e != nil {
				// 替换响应的错误为内部错误
				err = status.Error(codes.Internal, "")
			}
		}()
		err = handler(srv, ss)
		return err
	}
}

// 记录grpc调用的日志，当返回的错误不为空时，需要响应内部错误
func logGRPC(ctx context.Context, req interface{}, headers map[string][]string, method string, start time.Time, handlerError error) error {
	// 构建grpc调用信息
	rpcInfo := gRPCInfo{
		Headers:       requestHeaderFilter(headers),
		RequestAPI:    method,
		RemoteAddr:    IPFromGRPCContext(ctx),
		ExecutionTime: fmt.Sprintf("%vms", time.Since(start).Nanoseconds()/1e6),
		Parameter:     req,
		Response: &response{
			ProtocolCode: int(codes.OK),
			Code:         codes.OK.String(),
		},
	}
	// 执行成功，直接响应
	if handlerError == nil {
		log.Info(rpcInfo)
		return nil
	}

	// 执行handler失败，解析错误
	// 尝试解析grpc标准库error
	if s, ok := status.FromError(handlerError); ok {
		rpcInfo.Response = &response{
			ProtocolCode: int(s.Code()),
			Code:         s.Code().String(),
			Message:      s.Message(),
		}
		switch s.Code() {
		case codes.Internal:
			log.Error(rpcInfo)
		default:
			log.Warn(rpcInfo)
		}
		return nil
	}

	// 非标准库error，直接打印
	log.Error(handlerError)
	// 打印grpc调用日志
	rpcInfo.Response = &response{
		ProtocolCode: int(codes.Internal),
		Code:         codes.Internal.String(),
		Message:      handlerError.Error(),
	}
	log.Error(rpcInfo)
	// 内部错误
	return handlerError
}

// IPFromGRPCContext 从GRPC上下文中获取客户端ip地址
func IPFromGRPCContext(ctx context.Context) string {
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		forwards := headers.Get("x-forwarded-for")
		if len(forwards) != 0 {
			return net.ParseIP(forwards[0]).String()
		}
	}
	var clientIP string
	p, ok := peer.FromContext(ctx)
	if ok {
		// 默认是IPV4
		clientIP = common.TrimIP(p.Addr.String())
	}
	return clientIP
}
