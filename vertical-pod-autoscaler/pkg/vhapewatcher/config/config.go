package config

import (
	"flag"
	"fmt"
)

const (
	DefaultWorkerCount           = 1
	DefaultVhapeRecommenderName  = "vhape-recommender"
	DefaultVhapePolicyAnnotation = "vhape/policy"
	DefaultVhapePolicyNamespace  = "kube-system"
	DefaultVhapePolicyName       = "vhape-policy-p93-default"
)

type Config struct {
	WorkerCount                 int
	VhapeRecommenderName        string
	VhapePolicyAnnotation       string
	DefaultVhapePolicyNamespace string
	DefaultVhapePolicyName      string
}

func ParseFlags() (Config, error) {
	cfg := Config{
		WorkerCount:                 DefaultWorkerCount,
		VhapeRecommenderName:        DefaultVhapeRecommenderName,
		VhapePolicyAnnotation:       DefaultVhapePolicyAnnotation,
		DefaultVhapePolicyNamespace: DefaultVhapePolicyNamespace,
		DefaultVhapePolicyName:      DefaultVhapePolicyName,
	}

	flag.IntVar(
		&cfg.WorkerCount,
		"workers",
		DefaultWorkerCount,
		"Number of worker goroutines used by VHAPE Watcher.",
	)

	flag.StringVar(
		&cfg.DefaultVhapePolicyNamespace,
		"default-vhape-policy-namespace",
		DefaultVhapePolicyNamespace,
		"Namespace of the default VhapePolicy used by VPAs created by VHAPE Watcher.",
	)

	flag.StringVar(
		&cfg.DefaultVhapePolicyName,
		"default-vhape-policy-name",
		DefaultVhapePolicyName,
		"Name of the default VhapePolicy used by VPAs created by VHAPE Watcher.",
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
	if c.VhapeRecommenderName == "" {
		return fmt.Errorf("vhape recommender name is empty")
	}
	if c.VhapePolicyAnnotation == "" {
		return fmt.Errorf("vhape policy annotation is empty")
	}
	if c.DefaultVhapePolicyNamespace == "" {
		return fmt.Errorf("default VhapePolicy namespace is empty")
	}
	if c.DefaultVhapePolicyName == "" {
		return fmt.Errorf("default VhapePolicy name is empty")
	}

	return nil
}