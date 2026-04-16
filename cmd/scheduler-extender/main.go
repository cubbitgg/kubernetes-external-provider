package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cubbitgg/kubernetes-external-provider/internal/scheduler"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)

	addr := flag.String("addr", ":8888", "Address to listen on")
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

	factory := informers.NewSharedInformerFactory(client, 10*time.Minute)
	pvcInformer := factory.Core().V1().PersistentVolumeClaims()

	ext := scheduler.NewExtender(client, pvcInformer.Lister())

	mux := http.NewServeMux()
	mux.HandleFunc("/filter", ext.Filter)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			klog.ErrorS(err, "HTTP server shutdown error")
		}
	}()

	klog.InfoS("Starting scheduler extender", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.ErrorS(err, "HTTP server error")
		os.Exit(1)
	}
}
