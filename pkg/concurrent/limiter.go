package concurrent

type Limiter interface {
	// Add enqueue one working credential.
	Add()
	// Done dequeue one working credential.
	Done()
}

type limiter struct {
	working chan struct{}
}

func NewLimiter(maxConcurrency int) Limiter {
	return &limiter{
		working: make(chan struct{}, 10),
	}
}

func (in *limiter) Add() {
	in.working <- struct{}{}
}

func (in *limiter) Done() {
	<-in.working
}
