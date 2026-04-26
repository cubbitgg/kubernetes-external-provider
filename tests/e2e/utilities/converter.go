package utilities

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ctrf-io/go-ctrf-json-reporter/ctrf"
	"github.com/kudobuilder/kuttl/pkg/report"
)

// ConvertKuttlReportToCTRF converts KUTTL JSON report to CTRF format
func ConvertKuttlReportToCTRF(t *testing.T, kuttlReportPath, ctrfOutputPath string) error {
	// Read the KUTTL JSON report
	reportData, err := os.ReadFile(kuttlReportPath)
	if err != nil {
		return fmt.Errorf("failed to read KUTTL report: %w", err)
	}
	t.Logf("reportData:%s", string(reportData))
	// Parse the KUTTL report
	var kuttlReport report.Testsuites
	if err := json.Unmarshal(reportData, &kuttlReport); err != nil {
		return fmt.Errorf("failed to parse KUTTL report: %w", err)
	}

	// Create CTRF suites array
	env := ctrf.Environment{
		AppName: "kuttl go test",
	}

	testSuiteCounter := len(kuttlReport.Testsuite)
	t.Logf("testsuite from report kuttl:%d", testSuiteCounter)
	suite := kuttlReport.Testsuite[0]
	duration, err := time.ParseDuration(suite.Time + "s")
	if err != nil {
		return err
	}
	testCasesCounter := len(suite.SubSuites)
	t.Logf("testcase counter from report kuttl:%d", testCasesCounter)

	var tests []*ctrf.TestResult
	for _, subsuite := range suite.SubSuites {
		// Compute suite-level total duration.
		subsuiteDuration, err := time.ParseDuration(subsuite.Time + "s")
		if err != nil {
			return err
		}

		ctrfTest := &ctrf.TestResult{
			Name:     subsuite.Name,
			Suite:    suite.Name,
			Status:   ctrf.TestPassed,
			Duration: int64(subsuiteDuration.Milliseconds()),
			Start:    subsuite.Timestamp.UnixMilli(),
			Stop:     subsuite.Timestamp.Add(subsuiteDuration).UnixMilli(),
		}

		if subsuite.Failures > 0 {
			ctrfTest.Status = ctrf.TestFailed

			// KUTTL doesn't have a standard way to report subsuite-level failure messages,
			// but in practice we see that when a subsuite fails, its first testcase contains the failure message and trace.
			for _, test := range subsuite.Testcases {
				if test.Failure != nil {
					ctrfTest.Message = test.Failure.Message
					ctrfTest.Trace = test.Failure.Text
					break
				}
			}
		}
		tests = append(tests, ctrfTest)
	}

	report := ctrf.NewReport("kuttl-tests", &env)
	testCounter := len(suite.SubSuites)
	report.Results = &ctrf.Results{
		Tool: &ctrf.Tool{
			Name: "kuttl",
		},
		Summary: &ctrf.Summary{
			Tests:  testCounter,
			Passed: testCounter - suite.Failures,
			Failed: suite.Failures,
			Suites: 1,
			Start:  suite.Timestamp.UnixMilli(),
			Stop:   suite.Timestamp.Add(duration).UnixMilli(),
		},
		Tests: tests,
	}

	if err := report.WriteFile(ctrfOutputPath); err != nil {
		return err
	}
	t.Logf("Generated CTRF report: %s", ctrfOutputPath)

	return nil
}
