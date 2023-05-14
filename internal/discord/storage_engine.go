package discord

import (
	"context"
	"moff.io/moff-social/pkg/log"
	"sync"
)

type singleWriteStorageEngine struct {
	pipeline chan func()
}

var (
	initStorageEngineOnce sync.Once
	internalStorageEngine *singleWriteStorageEngine
)

func NewSingleWriteStorageEngine() *singleWriteStorageEngine {
	initStorageEngineOnce.Do(func() {
		internalStorageEngine = &singleWriteStorageEngine{
			pipeline: make(chan func(), 20000),
		}
	})
	return internalStorageEngine
}

func (in *singleWriteStorageEngine) Enqueue(writer func()) {
	in.pipeline <- writer
}

func (in *singleWriteStorageEngine) Start(ctx context.Context) {
	go in.start(ctx)
}

func (in *singleWriteStorageEngine) start(ctx context.Context) {
	log.Info("Single write storage engine running...")
	defer log.Info("Single write storage engine stopped...")
	for {
		select {
		case <-ctx.Done():
			return
		case fn := <-in.pipeline:
			fn()
		}
	}
}
