package errors

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func IsGRpcError(err error, code ...codes.Code) bool {
	if err == nil {
		return false
	}
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	for _, c := range code {
		if s.Code() == c {
			return true
		}
	}
	return false
}
