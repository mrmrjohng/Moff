package middleware

type grpcConfig struct {
	// 日志不记录unary请求参数的方法集，映射关系 method => true
	noLogUnaryRequestParamMethods map[string]bool
}

// InterceptorOption 拦截器选项
type InterceptorOption func(*grpcConfig)

func defaultGRPCConfig() *grpcConfig {
	return &grpcConfig{
		noLogUnaryRequestParamMethods: make(map[string]bool),
	}
}

// NoUnaryRequestParamsLog 不记录gprc unary方法的请求参数
func NoUnaryRequestParamsLog(methods ...string) InterceptorOption {
	return func(c *grpcConfig) {
		for _, m := range methods {
			c.noLogUnaryRequestParamMethods[m] = true
		}
	}
}
