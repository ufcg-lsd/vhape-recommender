package config

import (
	"flag"
	"fmt"
)

const (
	DefaultWorkerCount           = 1
)

type Config struct {
	WorkerCount                 int
}

func ParseFlags() (Config, error) {
	var cfg Config

	flag.IntVar(
		&cfg.WorkerCount,
		"workers",
		DefaultWorkerCount,
		"Number of worker goroutines used by VHAPE Watcher.",
	)

	flag.Parse()

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.WorkerCount <= 0 {
		return fmt.Errorf("worker count must be greater than zero")
	}

	return nil
}