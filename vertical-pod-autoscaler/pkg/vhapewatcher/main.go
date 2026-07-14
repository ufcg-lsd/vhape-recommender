package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"k8s.io/autoscaler/vertical-pod-autoscaler/common"
	vhapewatcherclient "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/client"
	watcherconfig "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/config"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
	reconciler "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/reconciler"
	watcherscope "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/scope"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	watcherhandlers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/watchers"
)

func main() {
	commonFlags := common.InitCommonFlags()

	klog.InitFlags(nil)
	defer klog.Flush()

	cfg, err := watcherconfig.ParseFlags()
	if err != nil {
		klog.ErrorS(err, "Invalid VHAPE Watcher configuration")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	klog.InfoS(
		"Starting VHAPE Watcher",
		"workerCount", cfg.WorkerCount,
		"defaultVhapePolicyNamespace", cfg.DefaultVhapePolicyNamespace,
		"defaultVhapePolicyName", cfg.DefaultVhapePolicyName,
		"defaultVPAUpdateMode", cfg.VPAUpdateMode,
	)

	if err := run(ctx, commonFlags, cfg); err != nil {
		klog.ErrorS(err, "VHAPE Watcher failed")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	klog.InfoS("VHAPE Watcher stopped")
}

func run(
	ctx context.Context,
	commonFlags *common.CommonFlags,
	cfg watcherconfig.Config,
) error {
	if commonFlags == nil {
		return fmt.Errorf("common flags are nil")
	}

	kubeConfig := common.CreateKubeConfigOrDie(
		commonFlags.KubeConfig,
		float32(commonFlags.KubeApiQps),
		int(commonFlags.KubeApiBurst),
	)

	clients, err := vhapewatcherclient.NewClients(kubeConfig)
	if err != nil {
		return fmt.Errorf("create clients: %w", err)
	}

	informerSet, err := watcherinformers.New(clients)
	if err != nil {
		return fmt.Errorf("create informers: %w", err)
	}

	scopeResolver, err := watcherscope.New(
		informerSet.VhapeWatchedNamespace.Lister(),
		informerSet.VhapeIgnoredWorkload.Lister(),
	)
	if err != nil {
		return fmt.Errorf("create scope resolver: %w", err)
	}

	vpaService, err := vpaservice.NewVPAService(
		informerSet.VPA,
		clients.Vhape,
		cfg,
	)
	if err != nil {
		return fmt.Errorf("create VPA service: %w", err)
	}

	reconcilerObj, err := reconciler.New(
		informerSet.Deployment.Lister(),
		scopeResolver,
		vpaService,
	)
	if err != nil {
		return fmt.Errorf("create reconciler: %w", err)
	}

	watchers, err := watcherhandlers.New(reconcilerObj)
	if err != nil {
		return fmt.Errorf("create watchers: %w", err)
	}

	if err := watchers.Register(
		informerSet.Deployment,
		informerSet.VPA,
		informerSet.VhapeWatchedNamespace,
		informerSet.VhapeIgnoredWorkload,
	); err != nil {
		return fmt.Errorf("register watchers: %w", err)
	}

	klog.InfoS("Starting informer factories")
	informerSet.Start(ctx.Done())

	klog.InfoS("Waiting for informer caches to sync")
	if err := informerSet.WaitForCacheSync(ctx.Done()); err != nil {
		return fmt.Errorf("wait for cache sync: %w", err)
	}

	klog.InfoS("Informer caches synced")

	reconcilerObj.Run(ctx, cfg.WorkerCount)

	return nil
}