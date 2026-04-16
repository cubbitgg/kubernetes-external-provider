package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/cubbitgg/kubernetes-external-provider/internal/provisioner"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v13/controller"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	cfg, err := rest.InClusterConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to build in-cluster config")
		os.Exit(1)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.ErrorS(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	p := provisioner.New(client)

	pc := controller.NewProvisionController(
		ctx,
		client,
		"agent.cubbit.io/local-disk",
		p,
		controller.LeaderElection(true),
		controller.FailedProvisionThreshold(10),
		controller.MetricsPort(8080),
		controller.ExponentialBackOffOnError(true),
	)

	klog.InfoS("Starting local-disk provisioner")
	pc.Run(ctx)
}
