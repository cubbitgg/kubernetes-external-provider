package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cubbitgg/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/internal/webhook"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	webhookConfigName  = "local-disk-webhook"
	webhookEntryName   = "disk-affinity.agent.cubbit.io"
	webhookSecretName  = "local-disk-webhook-tls"
	webhookServiceName = "local-disk-webhook"
	webhookNamespace   = "kube-system"
)

func main() {
	addr := flag.String("addr", ":8443", "HTTPS listen address")
	certFile := flag.String("tls-cert", "/certs/tls.crt", "TLS certificate file (unused when --self-sign is set)")
	keyFile := flag.String("tls-key", "/certs/tls.key", "TLS private key file (unused when --self-sign is set)")
	selfSign := flag.Bool("self-sign", false, "Generate a self-signed certificate, store it in a Kubernetes Secret, and patch the MutatingWebhookConfiguration")
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

	var tlsCert tls.Certificate

	if *selfSign {
		caCert, certPEM, keyPEM, err := webhook.EnsureTLSSecret(ctx, client, webhookSecretName, webhookNamespace, webhookServiceName)
		if err != nil {
			log.Error().Err(err).Msg("Failed to ensure TLS secret")
			os.Exit(1)
		}
		if err := webhook.PatchWebhookCABundle(ctx, client, webhookConfigName, webhookEntryName, caCert); err != nil {
			log.Error().Err(err).Msg("Failed to patch MutatingWebhookConfiguration caBundle")
			os.Exit(1)
		}
		tlsCert, err = tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			log.Error().Err(err).Msg("Failed to parse self-signed certificate")
			os.Exit(1)
		}
		log.Info().Msg("Using self-signed TLS certificate from Kubernetes Secret")
	} else {
		tlsCert, err = tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			log.Error().Err(err).Str("cert", *certFile).Str("key", *keyFile).Msg("Failed to load TLS certificate from file")
			os.Exit(1)
		}
		log.Info().Str("cert", *certFile).Msg("Using file-based TLS certificate")
	}

	factory := informers.NewSharedInformerFactory(client, 10*time.Minute)
	pvcInformer := factory.Core().V1().PersistentVolumeClaims()
	nodeInformer := factory.Core().V1().Nodes()

	wh := webhook.New(client, pvcInformer.Lister(), nodeInformer.Lister(), log)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", wh.Handle)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprintln(w, "ok")
		if err != nil {
			log.Error().Err(err).Msg("Failed to write healthz response")
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	srv := &http.Server{
		Addr:    *addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
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
			log.Error().Err(err).Msg("HTTPS server shutdown error")
		}
	}()

	log.Info().Str("addr", *addr).Msg("Starting admission webhook")
	if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("HTTPS server error")
		os.Exit(1)
	}
}
