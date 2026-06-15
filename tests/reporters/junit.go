// Package reporters writes test results in JUnit XML and JSON formats.
package reporters

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yasserrmd/nuraos/tests/harness"
)

// JUnit XML document structure.
type xmlTestsuites struct {
	XMLName    xml.Name       `xml:"testsuites"`
	Testsuites []xmlTestsuite `xml:"testsuite"`
}

type xmlTestsuite struct {
	Name      string        `xml:"name,attr"`
	Tests     int           `xml:"tests,attr"`
	Failures  int           `xml:"failures,attr"`
	Skipped   int           `xml:"skipped,attr"`
	Time      float64       `xml:"time,attr"`
	Testcases []xmlTestcase `xml:"testcase"`
}

type xmlTestcase struct {
	Name      string      `xml:"name,attr"`
	Classname string      `xml:"classname,attr"`
	Time      float64     `xml:"time,attr"`
	Failure   *xmlFailure `xml:"failure,omitempty"`
	Skipped   *struct{}   `xml:"skipped,omitempty"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// WriteJUnit writes a JUnit XML report for run to dir/<suite>-junit.xml.
// GitHub Actions and most CI systems consume this format for test annotations.
func WriteJUnit(dir string, run harness.SuiteRun) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating report dir: %w", err)
	}

	suite := xmlTestsuite{
		Name:  run.Suite,
		Tests: len(run.Results),
	}
	totalSec := 0.0

	for _, r := range run.Results {
		durationSec := r.Duration / 1000.0
		totalSec += durationSec
		tc := xmlTestcase{
			Name:      r.Case,
			Classname: run.Suite,
			Time:      durationSec,
		}
		switch r.Status {
		case harness.StatusFail:
			suite.Failures++
			tc.Failure = &xmlFailure{Message: r.Message, Body: r.Message}
		case harness.StatusSkip:
			suite.Skipped++
			tc.Skipped = &struct{}{}
		}
		suite.Testcases = append(suite.Testcases, tc)
	}
	suite.Time = totalSec

	root := xmlTestsuites{Testsuites: []xmlTestsuite{suite}}
	data, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JUnit XML: %w", err)
	}

	out := filepath.Join(dir, run.Suite+"-junit.xml")
	content := append([]byte(xml.Header), data...)
	return os.WriteFile(out, content, 0o644)
}
