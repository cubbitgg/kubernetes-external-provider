package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cubbitgg/kubernetes-external-provider/internal/common"
	"github.com/cubbitgg/kubernetes-external-provider/internal/scanner"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)

	scanInterval := flag.Duration("scan-interval", 60*time.Second, "How often to scan for block devices")
	mountBase := flag.String("mount-base", common.DefaultMountBase, "Base directory for disk mounts")
	fsType := flag.String("fs-type", common.DefaultFSType, "Filesystem type to scan/mount/format")
	minSize := flag.Uint64("min-size", common.DefaultMinSize, "Minimum device size in bytes to consider")

	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		klog.Error("NODE_NAME environment variable is required (set via downward API)")
		os.Exit(1)
	}

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

	s := scanner.New(scanner.Config{
		NodeName:     nodeName,
		MountBase:    *mountBase,
		FSType:       *fsType,
		MinSize:      *minSize,
		ScanInterval: *scanInterval,
	}, client)

	klog.InfoS("Starting node scanner", "node", nodeName, "interval", *scanInterval)
	if err := s.Run(ctx); err != nil {
		klog.ErrorS(err, "Node scanner exited with error")
		os.Exit(1)
	}
}
