package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/tests/e2e/utilities"
	"github.com/kudobuilder/kuttl/pkg/apis/testharness/v1beta1"
	"github.com/kudobuilder/kuttl/pkg/test"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestE2E(t *testing.T) {
	if os.Getenv("KUTTL_TEST") != "true" {
		t.Skip("skipping kuttl e2e tests; set KUTTL_TEST=true to run")
	}

	version := os.Getenv("E2E_VERSION")
	if version == "" {
		version = "latest"
	}

	artifactsDir := "./artifacts"

	testSuite := v1beta1.TestSuite{
		TestDirs:    []string{"./testdata"},
		StartKIND:   true,
		KINDConfig:  "./kind-config.yaml",
		KINDContext: "kuttl-e2e",
		KINDContainers: []string{
			"docker.io/gigiozzz/local-disk-provisioner:" + version,
			"docker.io/gigiozzz/local-disk-webhook:" + version,
		},
		//SkipClusterDelete: true,
		SkipDelete:   true,
		Namespace:    "kuttl-e2e",
		Timeout:      60,
		Parallel:     1,
		ArtifactsDir: artifactsDir,
		ReportFormat: "JSON",
		ReportName:   "kuttl-report",
	}

	harness := test.Harness{
		TestSuite: testSuite,
		T:         t,
	}

	log.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stdout)))
	harness.Run()
	harness.Report()

	if err := utilities.ConvertKuttlReportToCTRF(t,
		filepath.Join(artifactsDir, "kuttl-report.json"),
		filepath.Join(artifactsDir, "ctrf-report.json"),
	); err != nil {
		t.Logf("WARNING: failed to generate CTRF report: %v", err)
	}
}
