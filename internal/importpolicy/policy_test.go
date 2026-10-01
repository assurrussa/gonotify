package importpolicy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/assurrussa/gonotify/internal/importpolicy"
	"github.com/assurrussa/gonotify/reference/externalconsumer"
)

const backendConsumerRoot = "backend"

func TestCheckPassesCurrentSupportedImports(t *testing.T) {
	t.Helper()

	repoRoot := t.TempDir()
	writeGoFile(t, repoRoot, "backend/good.go", `
package backend

import (
	_ "github.com/assurrussa/gonotify"
	_ "github.com/assurrussa/gonotify/di"
	_ "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	_ "github.com/assurrussa/gonotify/templates"
	_ "github.com/assurrussa/gonotify/transport"
	_ "github.com/assurrussa/gonotify/transport/notifyhub"
)
`)

	report, err := importpolicy.Check(importpolicy.Config{
		RepoRoot:          repoRoot,
		ConsumerRoots:     []string{backendConsumerRoot},
		SupportedPackages: externalconsumer.SupportedPackages,
	})
	require.NoError(t, err)
	require.True(t, report.OK(), report.Message())
}

func TestCheckRejectsUnsupportedImport(t *testing.T) {
	t.Helper()

	repoRoot := t.TempDir()
	writeGoFile(t, repoRoot, "backend/bad.go", `
package backend

import _ "github.com/assurrussa/gonotify/email"
`)

	report, err := importpolicy.Check(importpolicy.Config{
		RepoRoot:          repoRoot,
		ConsumerRoots:     []string{backendConsumerRoot},
		SupportedPackages: externalconsumer.SupportedPackages,
	})
	require.NoError(t, err)
	require.False(t, report.OK())
	require.Equal(t, []importpolicy.UnsupportedImport{{
		File:       "backend/bad.go",
		ImportPath: "github.com/assurrussa/gonotify/email",
	}}, report.UnsupportedImports)
}

func TestCheckIgnoresTestGeneratedAndMockImports(t *testing.T) {
	t.Helper()

	repoRoot := t.TempDir()
	writeGoFile(t, repoRoot, "backend/bad_test.go", `
package backend

import _ "github.com/assurrussa/gonotify/email"
`)
	writeGoFile(t, repoRoot, "backend/api.gen.go", `
package backend

import _ "github.com/assurrussa/gonotify/email"
`)
	writeGoFile(t, repoRoot, "backend/mocks/file.go", `
package mocks

import _ "github.com/assurrussa/gonotify/email"
`)

	report, err := importpolicy.Check(importpolicy.Config{
		RepoRoot:          repoRoot,
		ConsumerRoots:     []string{backendConsumerRoot},
		SupportedPackages: externalconsumer.SupportedPackages,
	})
	require.NoError(t, err)
	require.True(t, report.OK(), report.Message())
}

func writeGoFile(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600))
}
