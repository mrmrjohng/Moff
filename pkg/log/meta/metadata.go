package meta

import (
	"context"
	"github.com/sirupsen/logrus"
	"sync"
)

// 元信息对象
type metadata struct {
	// 同步map，确保并发安全
	carrier map[interface{}]interface{}
	mu      sync.RWMutex
}

func (c *metadata) Value(key interface{}) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.carrier[key]
}

func (c *metadata) WithValue(key, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.carrier[key] = value
}

type contextKey struct{}

var metaContextKey = contextKey{}

// Begin 开启元信息对象
// 注意：
// 		1.入参上下文必须是官方标准库定义的context.Context对象，如HTTP请求的上下文或grpc请求的上下文
//		2.该方法在整个上下文的对象中注入元信息对象，应该在尽量靠近根上下文处调用
//		3.多次调用数据安全：
//			如父类上下文中存在元信息对象，则直接返回父类上下文
//			如父类上下文中不存在元信息对象，则返回包含元信息对象的指针的子类上下文
func Begin(parent context.Context) context.Context {
	value := parent.Value(metaContextKey)
	if value == nil {
		meta := &metadata{
			carrier: make(map[interface{}]interface{}),
		}
		child := context.WithValue(parent, metaContextKey, meta)
		return child
	}
	return parent
}

// 从父类上下文获取元信息对象
func metadataFrom(parent context.Context) *metadata {
	value := parent.Value(metaContextKey)
	if value == nil {
		logrus.Debug("meta not found from context, should call meta.Begin() first?")
		return nil
	}
	return value.(*metadata)
}

// WithValue 设置键值对至上下文的元信息对象
func WithValue(parent context.Context, key, val interface{}) {
	meta := metadataFrom(parent)
	if meta == nil {
		return
	}
	meta.WithValue(key, val)
}

// Value 从上下文的元信息对象中获取对应key的值
func Value(parent context.Context, key interface{}) interface{} {
	meta := metadataFrom(parent)
	if meta == nil {
		return nil
	}
	return meta.Value(key)
}
