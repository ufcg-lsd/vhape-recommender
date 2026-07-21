package config

import (
	"flag"
	"fmt"
)

const (
	DefaultWorkerCount = 1
	DefaultRecommenderName = "vhape-recommender"
)

type Config struct {
	WorkerCount     int
	RecommenderName string
}

func ParseFlags() (Config, error) {
	var cfg Config

	flag.IntVar(
		&cfg.WorkerCount,
		"workers",
		DefaultWorkerCount,
		"Number of worker goroutines used by VHAPE Watcher.",
	)

	flag.StringVar(
		&cfg.RecommenderName,
		"recommender-name",
		DefaultRecommenderName,
		"Name of the VHAPE Recommender used by generated VPA objects.",
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

	if c.RecommenderName == "" {
		return fmt.Errorf("recommender name must not be empty")
	}

	return nil
}
