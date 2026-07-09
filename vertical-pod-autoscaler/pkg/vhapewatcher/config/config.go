package config

import (
	"flag"
	"fmt"
)

const (
	DefaultDryRun = false
	DefaultVhapeRecommenderName = "vhape-recommender"
	DefaultVhapePolicyAnnotation = "vhape/policy"
	DefaultVhapePolicyNamespace = "kube-system"
	DefaultVhapePolicyName = "vhape-policy-p93-default"
)

type Config struct {
	DryRun bool
	VhapeRecommenderName  string
	VhapePolicyAnnotation string
	DefaultVhapePolicyNamespace string
	DefaultVhapePolicyName      string
}

func ParseFlags() (Config, error) {
	cfg := Config{
		DryRun: DefaultDryRun,
		VhapeRecommenderName:  DefaultVhapeRecommenderName,
		VhapePolicyAnnotation: DefaultVhapePolicyAnnotation,
		DefaultVhapePolicyNamespace: DefaultVhapePolicyNamespace,
		DefaultVhapePolicyName: DefaultVhapePolicyName,
	}

	flag.BoolVar(
		&cfg.DryRun,
		"dry-run",
		DefaultDryRun,
		"If true, VHAPE Watcher logs actions without creating or updating Kubernetes objects.",
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