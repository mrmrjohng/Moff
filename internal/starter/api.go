package starter

import (
	"context"
	"moff.io/moff-social/internal/config"
)

type Startable interface {
	Start(ctx context.Context)
}

type Configurable interface {
	Apply(*config.Configuration)
}

func Start(ctx context.Context, elems ...Startable) {
	for _, ele := range elems {
		if configurable, ok := ele.(Configurable); ok {
			configurable.Apply(config.Global)
		}
		ele.Start(ctx)
	}
}

type Stopable interface {
	Stop()
}
