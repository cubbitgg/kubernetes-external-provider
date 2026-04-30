package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/provisioner/internal/provisioner"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v13/controller"
)

func main() {
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	flag.Parse()

	log := logger.InitLogger(*logLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Error().Err(err).Msg("Failed to build in-cluster config")
		os.Exit(1)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Kubernetes client")
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

	log.Info().Msg("Starting local-disk provisioner")
	pc.Run(ctx)
}
