package templates_test

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/assurrussa/gonotify/templates"
)

//go:embed testdata/email/*
var embeddedTemplates embed.FS

const (
	templateDataTitleKey          = "Title"
	templateDataUserNameKey       = "UserName"
	templateDataExpiresMinutesKey = "ExpiresMinutes"
	templateDataChannelRuKey      = "ChannelRu"
	templateDataCodeKey           = "Code"
	templateDataCodeSpacedKey     = "CodeSpaced"
	templateUserName              = "Тест"
	templateChannelRuEmail        = "Email"
	templateCodeSample            = "1 2 3"
)

func TestRender_ImmutableResultWithEmbedFS(t *testing.T) {
	subFS, err := fs.Sub(embeddedTemplates, "testdata/email")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}

	r := templates.New(subFS)
	data := map[string]any{
		templateDataTitleKey:          "Код подтверждения",
		templateDataUserNameKey:       "Alice",
		templateDataChannelRuKey:      templateChannelRuEmail,
		templateDataCodeKey:           "9 8 7 6",
		templateDataCodeSpacedKey:     "9 8 7 6",
		templateDataExpiresMinutesKey: 10,
	}

	content, err := r.Render("confirmation_code", data)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if content.Subject == "" {
		t.Fatal("expected non-empty subject")
	}
	if !strings.Contains(content.Subject, "Код подтверждения") {
		t.Fatalf("expected subject to contain title, got: %q", content.Subject)
	}

	if content.HTML == "" {
		t.Fatal("expected non-empty html")
	}
	if !strings.Contains(content.HTML, "Alice") {
		t.Fatalf("expected html to contain user name, got: %s", content.HTML)
	}
	if !strings.Contains(content.HTML, "9 8 7 6") {
		t.Fatalf("expected html to contain code, got: %s", content.HTML)
	}

	if content.Text == "" {
		t.Fatal("expected non-empty text")
	}
	if !strings.Contains(content.Text, "9 8 7 6") {
		t.Fatalf("expected text to contain code, got: %s", content.Text)
	}
}

func TestRender_HTMLAutoEscaping(t *testing.T) {
	subFS, err := fs.Sub(embeddedTemplates, "testdata/email")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}

	r := templates.NewCached(subFS)
	data := map[string]any{
		templateDataTitleKey:          "<script>alert('xss')</script>",
		templateDataUserNameKey:       "Bob & Charlie",
		templateDataChannelRuKey:      templateChannelRuEmail,
		templateDataCodeKey:           "1 2 3 4",
		templateDataCodeSpacedKey:     "1 2 3 4",
		templateDataExpiresMinutesKey: 5,
	}

	content, err := r.Render("confirmation_code", data)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	// HTML must escape <script> and &
	if strings.Contains(content.HTML, "<script>") {
		t.Fatalf("html should escape <script>, got raw tag in: %s", content.HTML)
	}
	if !strings.Contains(content.HTML, "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;") &&
		!strings.Contains(content.HTML, "&lt;script&gt;alert('xss')&lt;/script&gt;") {
		t.Fatalf("expected escaped script in html, got: %s", content.HTML)
	}
	if !strings.Contains(content.HTML, "Bob &amp; Charlie") {
		t.Fatalf("expected escaped & in html, got: %s", content.HTML)
	}

	// Text layout receives Title and should NOT be HTML-escaped
	if !strings.Contains(content.Text, "<script>alert('xss')</script>") {
		t.Fatalf("text should not be html-escaped, got: %s", content.Text)
	}
}

func TestRender_MissingTemplate_ReturnsNotExist(t *testing.T) {
	r := templates.NewDir("testdata/email")
	_, err := r.Render("non_existent_template", map[string]any{"Title": "X"})
	if err == nil {
		t.Fatal("expected error for non-existent template")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got: %v", err)
	}
}

func TestRender_SubjectOptional(t *testing.T) {
	workspaceDir := t.TempDir()
	tplDir := filepath.Join(workspaceDir, "templates")
	if err := os.MkdirAll(filepath.Join(tplDir, "no_subject"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tplDir, "no_subject", "txt.tmpl"), []byte("Hello text"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := templates.NewDir(tplDir)
	content, err := r.Render("no_subject", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Subject != "" {
		t.Fatalf("expected empty subject, got: %q", content.Subject)
	}
	if content.Text != "Hello text" {
		t.Fatalf("expected 'Hello text', got: %q", content.Text)
	}
}

func TestCachedRenderer_PreloadAndConcurrentRender(t *testing.T) {
	r := templates.NewCachedDir("testdata/email")
	if err := r.Preload(); err != nil {
		t.Fatalf("preload: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := map[string]any{
				templateDataTitleKey:          "Title",
				templateDataUserNameKey:       templateUserName,
				templateDataChannelRuKey:      templateChannelRuEmail,
				templateDataCodeKey:           templateCodeSample,
				templateDataCodeSpacedKey:     templateCodeSample,
				templateDataExpiresMinutesKey: 15,
			}
			res, err := r.Render("confirmation_code", data)
			if err != nil {
				t.Errorf("goroutine %d render: %v", idx, err)
				return
			}
			if !strings.Contains(res.HTML, "Никому не сообщайте этот код") {
				t.Errorf("goroutine %d content missing", idx)
			}
		}(i)
	}
	wg.Wait()
}

func TestCachedEqualsDirectFSRenderer(t *testing.T) {
	r1 := templates.NewDir("testdata/email")
	r2 := templates.NewCachedDir("testdata/email")

	data := map[string]any{
		templateDataTitleKey:          "Заголовок",
		templateDataUserNameKey:       templateUserName,
		templateDataChannelRuKey:      templateChannelRuEmail,
		templateDataCodeKey:           templateCodeSample,
		templateDataCodeSpacedKey:     templateCodeSample,
		templateDataExpiresMinutesKey: 15,
	}

	c1, err := r1.Render("confirmation_code", data)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := r2.Render("confirmation_code", data)
	if err != nil {
		t.Fatal(err)
	}

	if c1.HTML != c2.HTML {
		t.Fatalf("HTML mismatch between direct and cached renderers")
	}
	if c1.Text != c2.Text {
		t.Fatalf("Text mismatch between direct and cached renderers")
	}
	if c1.Subject != c2.Subject {
		t.Fatalf("Subject mismatch between direct and cached renderers")
	}
}

func TestRender_InvalidTemplateSyntax_ReturnsError(t *testing.T) {
	workspaceDir := t.TempDir()
	tplDir := filepath.Join(workspaceDir, "templates")
	if err := os.MkdirAll(filepath.Join(tplDir, "bad_syntax"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tplDir, "bad_syntax", "html.tmpl"), []byte("<p>{{.UnclosedTag</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := templates.NewDir(tplDir)
	_, err := r.Render("bad_syntax", nil)
	if err == nil {
		t.Fatal("expected parse error for invalid template syntax")
	}
}

func TestRender_MissingKeyError(t *testing.T) {
	subFS, err := fs.Sub(embeddedTemplates, "testdata/email")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}

	r := templates.New(subFS)
	// Missing UserName, Code, etc.
	data := map[string]any{
		templateDataTitleKey: "Title Only",
	}

	_, err = r.Render("confirmation_code", data)
	if err == nil {
		t.Fatal("expected error for missing map key with missingkey=error, got nil")
	}

	cachedR := templates.NewCached(subFS)
	_, err = cachedR.Render("confirmation_code", data)
	if err == nil {
		t.Fatal("expected error for missing map key with cached missingkey=error, got nil")
	}
}
