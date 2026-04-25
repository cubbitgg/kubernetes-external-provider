package webhook

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const certValidity = 10 * 365 * 24 * time.Hour

// GenerateSelfSignedCert generates a CA and a server certificate for the given service.
// The server cert has SANs for <service>.<namespace>.svc and <service>.<namespace>.svc.cluster.local.
func GenerateSelfSignedCert(serviceName, namespace string) (caCertPEM, serverCertPEM, serverKeyPEM []byte, err error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: serviceName + "-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(certValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create CA cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate server key: %w", err)
	}

	svc := serviceName + "." + namespace + ".svc"
	svcFull := serviceName + "." + namespace + ".svc.cluster.local"
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: svc},
		DNSNames:     []string{svc, svcFull},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(certValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create server cert: %w", err)
	}

	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	return caCertPEM, serverCertPEM, serverKeyPEM, nil
}

// EnsureTLSSecret gets the TLS secret if it exists, or generates self-signed certs and creates it.
// Returns caCert, serverCert, serverKey PEM bytes.
func EnsureTLSSecret(ctx context.Context, client kubernetes.Interface, secretName, namespace, serviceName string) (caCert, serverCert, serverKey []byte, err error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return secret.Data["ca.crt"], secret.Data["tls.crt"], secret.Data["tls.key"], nil
	}
	if !k8serrors.IsNotFound(err) {
		return nil, nil, nil, fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
	}

	caCert, serverCert, serverKey, err = GenerateSelfSignedCert(serviceName, namespace)
	if err != nil {
		return nil, nil, nil, err
	}

	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt":  caCert,
			"tls.crt": serverCert,
			"tls.key": serverKey,
		},
	}
	_, createErr := client.CoreV1().Secrets(namespace).Create(ctx, s, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(createErr) {
		// Another replica created it first; use its cert.
		secret, err = client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("get secret after race: %w", err)
		}
		return secret.Data["ca.crt"], secret.Data["tls.crt"], secret.Data["tls.key"], nil
	}
	if createErr != nil {
		return nil, nil, nil, fmt.Errorf("create secret %s/%s: %w", namespace, secretName, createErr)
	}

	return caCert, serverCert, serverKey, nil
}

// PatchWebhookCABundle updates the caBundle and sets failurePolicy=Fail for the named
// webhook entry in the MutatingWebhookConfiguration.
func PatchWebhookCABundle(ctx context.Context, client kubernetes.Interface, webhookConfigName, webhookEntryName string, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		wh, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get MutatingWebhookConfiguration %s: %w", webhookConfigName, err)
		}

		found := false
		for i := range wh.Webhooks {
			if wh.Webhooks[i].Name == webhookEntryName {
				wh.Webhooks[i].ClientConfig.CABundle = caBundle
				fp := admissionregistrationv1.Fail
				wh.Webhooks[i].FailurePolicy = &fp
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("webhook entry %q not found in MutatingWebhookConfiguration %s", webhookEntryName, webhookConfigName)
		}

		_, err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(ctx, wh, metav1.UpdateOptions{})
		return err
	})
}
