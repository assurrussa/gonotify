package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/assurrussa/gonotify/internal/importpolicy"
	"github.com/assurrussa/gonotify/reference/externalconsumer"
)

func main() {
	var repoRoot string
	var consumers csvFlag

	flag.StringVar(&repoRoot, "repo-root", "..", "repository root to scan")
	flag.Var(&consumers, "consumers", "comma-separated host consumer roots")
	flag.Parse()

	if len(consumers) == 0 {
		consumers = csvFlag{"backend", "goadmin", "goauth", "fixtures"}
	}

	report, err := importpolicy.Check(importpolicy.Config{
		RepoRoot:          repoRoot,
		ConsumerRoots:     consumers,
		SupportedPackages: externalconsumer.SupportedPackages,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "gonotify import policy check failed: %v\n", err)
		os.Exit(1)
	}
	if !report.OK() {
		_, _ = fmt.Fprintln(os.Stderr, report.Message())
		_, _ = fmt.Fprintln(os.Stderr)
		_, _ = fmt.Fprintln(os.Stderr, "Only packages listed in gonotify/reference/externalconsumer are stable for host consumers.")
		os.Exit(1)
	}
}

type csvFlag []string

func (f *csvFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *csvFlag) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		*f = append(*f, item)
	}
	return nil
}
