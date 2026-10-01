package templates_test

import (
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/assurrussa/gonotify/templates"
)

type failingFS struct {
	fs.FS
	path string
	err  error
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
	}
	return f.FS.Open(name)
}

func TestPreload_NestedSyntaxErrors(t *testing.T) {
	for _, filename := range []string{"html.tmpl", "txt.tmpl", "subject.tmpl"} {
		t.Run(filename, func(t *testing.T) {
			root := fstest.MapFS{"account/messages/welcome/" + filename: {Data: []byte("{{if")}}
			if err := templates.NewCached(root).Preload(); err == nil {
				t.Fatal("nested syntax error must fail startup preload")
			}
		})
	}
}

func TestPreload_NestedTemplatesAndPartials(t *testing.T) {
	root := fstest.MapFS{
		"account/welcome/html.tmpl":    {Data: []byte(`<p>{{template "greeting" .}}</p>`)},
		"account/welcome/txt.tmpl":     {Data: []byte("Hello {{.Name}}")},
		"account/welcome/subject.tmpl": {Data: []byte("Welcome {{.Name}}")},
		"partials/greeting.html.tmpl":  {Data: []byte(`{{define "greeting"}}Hello {{.Name}}{{end}}`)},
		"partials/ignored/html.tmpl":   {Data: []byte("{{if")},
		"account/notes.md":             {Data: []byte("{{if")},
	}
	r := templates.NewCached(root)
	if err := r.Preload(); err != nil {
		t.Fatal(err)
	}
	result, err := r.Render("account/welcome", map[string]any{"Name": templateUserName})
	if err != nil {
		t.Fatal(err)
	}
	if result.HTML != "<p>Hello "+templateUserName+"</p>" ||
		result.Text != "Hello "+templateUserName || result.Subject != "Welcome "+templateUserName {
		t.Fatalf("unexpected nested render: %+v", result)
	}
	if err := r.Preload(); err != nil {
		t.Fatalf("repeat preload after HTML execution: %v", err)
	}
}

func TestPreload_UndefinedReferences(t *testing.T) {
	for _, filename := range []string{"html.tmpl", "txt.tmpl", "subject.tmpl", "layout.html.tmpl", "layout.txt.tmpl"} {
		t.Run(filename, func(t *testing.T) {
			name := filename
			if !strings.HasPrefix(name, "layout.") {
				name = "account/welcome/" + filename
			}
			root := fstest.MapFS{name: {Data: []byte(`{{if .Enabled}}{{template "missing" .}}{{end}}`)}}
			if err := templates.NewCached(root).Preload(); err == nil {
				t.Fatal("undefined template reference must fail startup preload")
			}
		})
	}
}

func TestPreload_UnusedPartialSyntax(t *testing.T) {
	root := fstest.MapFS{"partials/broken.html.tmpl": {Data: []byte("{{if")}}
	if err := templates.NewCached(root).Preload(); err == nil {
		t.Fatal("invalid partial syntax must fail preload even without message templates")
	}
}

func TestPreload_ConcurrentWithRendering(t *testing.T) {
	root := fstest.MapFS{
		"account/welcome/html.tmpl": {Data: []byte(`<p>{{.Name}}</p>`)},
		"account/welcome/txt.tmpl":  {Data: []byte(`{{.Name}}`)},
	}
	r := templates.NewCached(root)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				if err := r.Preload(); err != nil {
					t.Errorf("concurrent preload: %v", err)
					return
				}
				if _, err := r.Render("account/welcome", map[string]any{"Name": templateUserName}); err != nil {
					t.Errorf("concurrent render: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestRender_PropagatesFilesystemErrors(t *testing.T) {
	for _, target := range []string{"welcome/html.tmpl", "layout.html.tmpl", "partials"} {
		t.Run(target, func(t *testing.T) {
			root := failingFS{FS: fstest.MapFS{
				"welcome/html.tmpl": {Data: []byte("Hello")},
				"welcome/txt.tmpl":  {Data: []byte("Hello")},
			}, path: target, err: fs.ErrPermission}
			for _, r := range []templates.Renderer{templates.New(root), templates.NewCached(root)} {
				if _, err := r.Render("welcome", nil); !errors.Is(err, fs.ErrPermission) {
					t.Fatalf("filesystem permission error was hidden: %v", err)
				}
			}
			if err := templates.NewCached(root).Preload(); !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("preload filesystem error was hidden: %v", err)
			}
		})
	}
}

func TestRenderSubject_MissingPartialIsNotOptional(t *testing.T) {
	root := failingFS{FS: fstest.MapFS{
		"welcome/subject.tmpl":           {Data: []byte(`{{template "greeting" .}}`)},
		"partials/greeting.subject.tmpl": {Data: []byte(`{{define "greeting"}}Hello{{end}}`)},
	}, path: "partials/greeting.subject.tmpl", err: fs.ErrNotExist}
	for _, r := range []templates.Renderer{templates.New(root), templates.NewCached(root)} {
		if _, err := r.RenderSubject("welcome", nil); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("missing required partial was treated as optional subject: %v", err)
		}
	}
}
