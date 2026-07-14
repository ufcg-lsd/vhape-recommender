package config

import (
	"flag"
	"fmt"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const (
	DefaultWorkerCount           = 1
	DefaultVhapePolicyNamespace  = "kube-system"
	DefaultVhapePolicyName       = "vhape-policy-p93-default"
	DefaultVPAUpdateMode         = "InPlaceOrRecreate"
)

type Config struct {
	WorkerCount                 int
	DefaultVhapePolicyNamespace string
	DefaultVhapePolicyName      string
	VPAUpdateMode               vpav1.UpdateMode
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

	updateMode := ""
	flag.StringVar(
		&updateMode,
		"default-vpa-update-mode",
		DefaultVPAUpdateMode,
		"Default VPA update mode used by VPAs created by VHAPE Watcher. Allowed values: Off, Initial, Recreate, InPlaceOrRecreate.",
	)

	flag.Parse()

	parsedUpdateMode, err := ParseVPAUpdateMode(updateMode)
	if err != nil {
		return Config{}, err
	}
	cfg.VPAUpdateMode = parsedUpdateMode

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func ParseVPAUpdateMode(value string) (vpav1.UpdateMode, error) {
	mode := vpav1.UpdateMode(value)

	switch mode {
	case vpav1.UpdateModeOff,
		vpav1.UpdateModeInitial,
		vpav1.UpdateModeRecreate,
		vpav1.UpdateModeInPlaceOrRecreate:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"invalid default VPA update mode %q, allowed values are: Off, Initial, Recreate, InPlaceOrRecreate",
			value,
		)
	}
}

func (c Config) Validate() error {
	if c.WorkerCount <= 0 {
		return fmt.Errorf("worker count must be greater than zero")
	}
	if c.DefaultVhapePolicyNamespace == "" {
		return fmt.Errorf("default VhapePolicy namespace is empty")
	}
	if c.DefaultVhapePolicyName == "" {
		return fmt.Errorf("default VhapePolicy name is empty")
	}

	return nil
}