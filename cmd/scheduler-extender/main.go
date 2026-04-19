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

	"github.com/cubbitgg/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/internal/scheduler"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	addr := flag.String("addr", ":8888", "Address to listen on")
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

	factory := informers.NewSharedInformerFactory(client, 10*time.Minute)
	pvcInformer := factory.Core().V1().PersistentVolumeClaims()

	ext := scheduler.NewExtender(client, pvcInformer.Lister(), log)

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
			log.Error().Err(err).Msg("HTTP server shutdown error")
		}
	}()

	log.Info().Str("addr", *addr).Msg("Starting scheduler extender")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("HTTP server error")
		os.Exit(1)
	}
}
