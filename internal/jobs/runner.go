package jobs

import (
	"context"
	"errors"
)

// Options 保存 worker 运行参数。
type Options struct {
	Concurrency int
}

// Runner 是 Phase 1 的后台任务骨架，后续承载 archive/index/hotmemory jobs。
type Runner struct {
	options Options
}

func NewRunner(options Options) Runner {
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}
	return Runner{options: options}
}

func NewRunnerChecked(options Options) (Runner, error) {
	if options.Concurrency <= 0 {
		return Runner{}, errors.New("worker concurrency must be positive")
	}
	return Runner{options: options}, nil
}

func (r Runner) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
