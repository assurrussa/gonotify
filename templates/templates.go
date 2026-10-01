package templates

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	texttemplate "text/template"
)

const (
	layoutContentKey = "Content"
	layoutDataKey    = "Data"
)

// RenderedContent represents the immutable result of rendering a notification template.
type RenderedContent struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// Renderer renders notification templates into immutable content strings.
type Renderer interface {
	Render(name string, data any) (RenderedContent, error)
	RenderHTML(name string, data any) (string, error)
	RenderText(name string, data any) (string, error)
	RenderSubject(name string, data any) (string, error)
}

// PreloadableRenderer compiles and caches templates upfront at startup.
type PreloadableRenderer interface {
	Renderer
	Preload() error
}

// New creates a direct, un-cached Renderer backed by any fs.FS (e.g., embed.FS or os.DirFS).
func New(root fs.FS) Renderer {
	return &fsRenderer{root: root}
}

// NewCached creates a thread-safe cached Renderer backed by any fs.FS.
func NewCached(root fs.FS) PreloadableRenderer {
	return &cachedRenderer{
		root:        root,
		htmlTpls:    make(map[string]*htmltemplate.Template),
		htmlLayouts: make(map[string]*htmltemplate.Template),
		textTpls:    make(map[string]*texttemplate.Template),
		textLayouts: make(map[string]*texttemplate.Template),
	}
}

// NewDir creates a direct, un-cached Renderer from a disk directory path.
func NewDir(dir string) Renderer {
	return New(os.DirFS(dir))
}

// NewCachedDir creates a thread-safe cached Renderer from a disk directory path.
func NewCachedDir(dir string) PreloadableRenderer {
	return NewCached(os.DirFS(dir))
}

// fsRenderer renders directly from filesystem on each call without caching.
type fsRenderer struct {
	root fs.FS
}

func (r *fsRenderer) Render(name string, data any) (RenderedContent, error) {
	var (
		res       RenderedContent
		foundPart bool
	)

	// HTML
	htmlPath := path.Join(name, "html.tmpl")
	hasHTML, err := exists(r.root, htmlPath)
	if err != nil {
		return res, err
	}
	if hasHTML {
		foundPart = true
		html, err := r.RenderHTML(name, data)
		if err != nil {
			return res, err
		}
		res.HTML = html
	}

	// Text
	textPath := path.Join(name, "txt.tmpl")
	hasText, err := exists(r.root, textPath)
	if err != nil {
		return res, err
	}
	if hasText {
		foundPart = true
		txt, err := r.RenderText(name, data)
		if err != nil {
			return res, err
		}
		res.Text = txt
	}

	// Subject
	subjectPath := path.Join(name, "subject.tmpl")
	hasSubject, err := exists(r.root, subjectPath)
	if err != nil {
		return res, err
	}
	if hasSubject {
		foundPart = true
		subj, err := r.RenderSubject(name, data)
		if err != nil {
			return res, err
		}
		res.Subject = subj
	}

	if !foundPart {
		return res, fmt.Errorf("%w: template %s", fs.ErrNotExist, name)
	}

	return res, nil
}

func (r *fsRenderer) RenderHTML(name string, data any) (string, error) {
	contentPath := path.Join(name, "html.tmpl")
	content, err := renderHTMLTemplate(r.root, contentPath, data)
	if err != nil {
		return "", err
	}
	layoutPath := "layout.html.tmpl"
	hasLayout, err := exists(r.root, layoutPath)
	if err != nil {
		return "", err
	}
	if hasLayout {
		return renderHTMLLayout(r.root, layoutPath, content, data)
	}
	return content, nil
}

func (r *fsRenderer) RenderText(name string, data any) (string, error) {
	contentPath := path.Join(name, "txt.tmpl")
	content, err := renderTextTemplate(r.root, contentPath, "txt", data)
	if err != nil {
		return "", err
	}
	layoutPath := "layout.txt.tmpl"
	hasLayout, err := exists(r.root, layoutPath)
	if err != nil {
		return "", err
	}
	if hasLayout {
		return renderTextLayout(r.root, layoutPath, "txt", content, data)
	}
	return content, nil
}

func (r *fsRenderer) RenderSubject(name string, data any) (string, error) {
	subjectPath := path.Join(name, "subject.tmpl")
	hasSubject, err := exists(r.root, subjectPath)
	if err != nil || !hasSubject {
		return "", err
	}
	s, err := renderTextTemplate(r.root, subjectPath, "subject", data)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// cachedRenderer caches compiled templates per name and extension.
type cachedRenderer struct {
	root        fs.FS
	mu          sync.RWMutex
	htmlTpls    map[string]*htmltemplate.Template
	htmlLayouts map[string]*htmltemplate.Template
	htmlBase    *htmltemplate.Template

	textTpls    map[string]*texttemplate.Template
	textLayouts map[string]*texttemplate.Template
	textBases   map[string]*texttemplate.Template
}

func (r *cachedRenderer) Render(name string, data any) (RenderedContent, error) {
	var (
		res       RenderedContent
		foundPart bool
	)

	// HTML
	htmlPath := path.Join(name, "html.tmpl")
	hasHTML, err := exists(r.root, htmlPath)
	if err != nil {
		return res, err
	}
	if hasHTML {
		foundPart = true
		html, err := r.RenderHTML(name, data)
		if err != nil {
			return res, err
		}
		res.HTML = html
	}

	// Text
	textPath := path.Join(name, "txt.tmpl")
	hasText, err := exists(r.root, textPath)
	if err != nil {
		return res, err
	}
	if hasText {
		foundPart = true
		txt, err := r.RenderText(name, data)
		if err != nil {
			return res, err
		}
		res.Text = txt
	}

	// Subject
	subjectPath := path.Join(name, "subject.tmpl")
	hasSubject, err := exists(r.root, subjectPath)
	if err != nil {
		return res, err
	}
	if hasSubject {
		foundPart = true
		subj, err := r.RenderSubject(name, data)
		if err != nil {
			return res, err
		}
		res.Subject = subj
	}

	if !foundPart {
		return res, fmt.Errorf("%w: template %s", fs.ErrNotExist, name)
	}

	return res, nil
}

func (r *cachedRenderer) RenderHTML(name string, data any) (string, error) {
	tpl, err := r.getOrCompileHTML(name)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, data); err != nil {
		return "", err
	}
	content := sb.String()

	layout, err := r.getOrCompileHTMLLayout()
	if err != nil {
		return "", err
	}
	if layout != nil {
		var lsb strings.Builder
		payload := map[string]any{
			//nolint:gosec // Content was rendered and escaped by html/template, safe to embed into layout.
			layoutContentKey: htmltemplate.HTML(content),
			layoutDataKey:    data,
		}
		if err := layout.Execute(&lsb, payload); err != nil {
			return "", err
		}
		return lsb.String(), nil
	}
	return content, nil
}

func (r *cachedRenderer) RenderText(name string, data any) (string, error) {
	tpl, err := r.getOrCompileText(name, "txt")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, data); err != nil {
		return "", err
	}
	content := sb.String()

	layout, err := r.getOrCompileTextLayout("txt")
	if err != nil {
		return "", err
	}
	if layout != nil {
		var lsb strings.Builder
		payload := map[string]any{
			layoutContentKey: content,
			layoutDataKey:    data,
		}
		if err := layout.Execute(&lsb, payload); err != nil {
			return "", err
		}
		return lsb.String(), nil
	}
	return content, nil
}

func (r *cachedRenderer) RenderSubject(name string, data any) (string, error) {
	hasSubject, err := exists(r.root, path.Join(name, "subject.tmpl"))
	if err != nil || !hasSubject {
		return "", err
	}
	tpl, err := r.getOrCompileText(name, "subject")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(sb.String()), nil
}

func (r *cachedRenderer) Preload() error {
	// Parse partials even when no message happens to use their engine yet.
	if _, err := r.getOrBuildHTMLBase(); err != nil {
		return err
	}
	for _, ext := range []string{"txt", "subject"} {
		if _, err := r.getOrBuildTextBase(ext); err != nil {
			return err
		}
	}
	if _, err := r.getOrCompileHTMLLayout(); err != nil {
		return err
	}
	if _, err := r.getOrCompileTextLayout("txt"); err != nil {
		return err
	}

	return fs.WalkDir(r.root, ".", func(templatePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if templatePath == "partials" {
				return fs.SkipDir
			}
			return nil
		}
		var err error
		switch path.Base(templatePath) {
		case "html.tmpl":
			_, err = r.getOrCompileHTML(path.Dir(templatePath))
		case "txt.tmpl":
			_, err = r.getOrCompileText(path.Dir(templatePath), "txt")
		case "subject.tmpl":
			_, err = r.getOrCompileText(path.Dir(templatePath), "subject")
		}
		return err
	})
}

func (r *cachedRenderer) getOrCompileHTML(name string) (*htmltemplate.Template, error) {
	r.mu.RLock()
	if t, ok := r.htmlTpls[name]; ok {
		r.mu.RUnlock()
		return t, nil
	}
	r.mu.RUnlock()

	base, err := r.getOrBuildHTMLBase()
	if err != nil {
		return nil, err
	}
	t, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone html base: %w", err)
	}

	tmplPath := path.Join(name, "html.tmpl")
	b, err := fs.ReadFile(r.root, tmplPath)
	if err != nil {
		return nil, err
	}
	if _, err := t.Parse(string(b)); err != nil {
		return nil, fmt.Errorf("parse html template %s: %w", tmplPath, err)
	}

	if err := validateHTMLReferences(t); err != nil {
		return nil, fmt.Errorf("validate html template %s: %w", tmplPath, err)
	}

	r.mu.Lock()
	r.htmlTpls[name] = t
	r.mu.Unlock()
	return t, nil
}

func (r *cachedRenderer) getOrCompileHTMLLayout() (*htmltemplate.Template, error) {
	r.mu.RLock()
	if t, ok := r.htmlLayouts["layout"]; ok {
		r.mu.RUnlock()
		return t, nil
	}
	r.mu.RUnlock()

	layoutPath := "layout.html.tmpl"
	hasLayout, err := exists(r.root, layoutPath)
	if err != nil {
		return nil, err
	}
	if !hasLayout {
		return nil, nil //nolint:nilnil // Missing layout is an intentional valid case returning no layout.
	}

	base, err := r.getOrBuildHTMLBase()
	if err != nil {
		return nil, err
	}
	t, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone html layout base: %w", err)
	}

	b, err := fs.ReadFile(r.root, layoutPath)
	if err != nil {
		return nil, err
	}
	if _, err := t.Parse(string(b)); err != nil {
		return nil, fmt.Errorf("parse html layout %s: %w", layoutPath, err)
	}

	if err := validateHTMLReferences(t); err != nil {
		return nil, fmt.Errorf("validate html layout %s: %w", layoutPath, err)
	}

	r.mu.Lock()
	r.htmlLayouts["layout"] = t
	r.mu.Unlock()
	return t, nil
}

func (r *cachedRenderer) getOrBuildHTMLBase() (*htmltemplate.Template, error) {
	r.mu.RLock()
	if r.htmlBase != nil {
		b := r.htmlBase
		r.mu.RUnlock()
		return b, nil
	}
	r.mu.RUnlock()

	t := htmltemplate.New("base-html").Option("missingkey=error")
	partials, err := partialPaths(r.root, "html")
	if err != nil {
		return nil, err
	}
	if len(partials) > 0 {
		for _, p := range partials {
			b, err := fs.ReadFile(r.root, p)
			if err != nil {
				return nil, fmt.Errorf("read html partial %s: %w", p, err)
			}
			if _, err := t.Parse(string(b)); err != nil {
				return nil, fmt.Errorf("parse html partial %s: %w", p, err)
			}
		}
	}

	r.mu.Lock()
	r.htmlBase = t
	r.mu.Unlock()
	return t, nil
}

func (r *cachedRenderer) getOrCompileText(name, ext string) (*texttemplate.Template, error) {
	key := ext + ":" + name
	r.mu.RLock()
	if t, ok := r.textTpls[key]; ok {
		r.mu.RUnlock()
		return t, nil
	}
	r.mu.RUnlock()

	base, err := r.getOrBuildTextBase(ext)
	if err != nil {
		return nil, err
	}
	t, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone text base: %w", err)
	}

	tmplPath := path.Join(name, ext+".tmpl")
	b, err := fs.ReadFile(r.root, tmplPath)
	if err != nil {
		return nil, err
	}
	if _, err := t.Parse(string(b)); err != nil {
		return nil, fmt.Errorf("parse text template %s: %w", tmplPath, err)
	}

	if err := validateTextReferences(t); err != nil {
		return nil, fmt.Errorf("validate text template %s: %w", tmplPath, err)
	}

	r.mu.Lock()
	r.textTpls[key] = t
	r.mu.Unlock()
	return t, nil
}

func (r *cachedRenderer) getOrCompileTextLayout(ext string) (*texttemplate.Template, error) {
	r.mu.RLock()
	if t, ok := r.textLayouts[ext]; ok {
		r.mu.RUnlock()
		return t, nil
	}
	r.mu.RUnlock()

	layoutPath := "layout." + ext + ".tmpl"
	hasLayout, err := exists(r.root, layoutPath)
	if err != nil {
		return nil, err
	}
	if !hasLayout {
		return nil, nil //nolint:nilnil // Missing layout is an intentional valid case returning no layout.
	}

	base, err := r.getOrBuildTextBase(ext)
	if err != nil {
		return nil, err
	}
	t, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone text layout base: %w", err)
	}

	b, err := fs.ReadFile(r.root, layoutPath)
	if err != nil {
		return nil, err
	}
	if _, err := t.Parse(string(b)); err != nil {
		return nil, fmt.Errorf("parse text layout %s: %w", layoutPath, err)
	}

	if err := validateTextReferences(t); err != nil {
		return nil, fmt.Errorf("validate text layout %s: %w", layoutPath, err)
	}

	r.mu.Lock()
	r.textLayouts[ext] = t
	r.mu.Unlock()
	return t, nil
}

func (r *cachedRenderer) getOrBuildTextBase(ext string) (*texttemplate.Template, error) {
	r.mu.RLock()
	if r.textBases != nil {
		if b, ok := r.textBases[ext]; ok {
			r.mu.RUnlock()
			return b, nil
		}
	}
	r.mu.RUnlock()

	t := texttemplate.New("base-text-" + ext).Option("missingkey=error")
	partials, err := partialPaths(r.root, ext)
	if err != nil {
		return nil, err
	}
	if len(partials) > 0 {
		for _, p := range partials {
			b, err := fs.ReadFile(r.root, p)
			if err != nil {
				return nil, fmt.Errorf("read text partial %s: %w", p, err)
			}
			if _, err := t.Parse(string(b)); err != nil {
				return nil, fmt.Errorf("parse text partial %s: %w", p, err)
			}
		}
	}

	r.mu.Lock()
	if r.textBases == nil {
		r.textBases = make(map[string]*texttemplate.Template)
	}
	r.textBases[ext] = t
	r.mu.Unlock()
	return t, nil
}

// Helpers for un-cached renderer

func renderHTMLTemplate(root fs.FS, tmplPath string, data any) (string, error) {
	t := htmltemplate.New("content").Option("missingkey=error")
	partials, err := partialPaths(root, "html")
	if err != nil {
		return "", err
	}
	if len(partials) > 0 {
		for _, p := range partials {
			b, err := fs.ReadFile(root, p)
			if err != nil {
				return "", fmt.Errorf("read partial %s: %w", p, err)
			}
			if _, err := t.Parse(string(b)); err != nil {
				return "", fmt.Errorf("parse partial %s: %w", p, err)
			}
		}
	}
	b, err := fs.ReadFile(root, tmplPath)
	if err != nil {
		return "", err
	}
	if _, err := t.Parse(string(b)); err != nil {
		return "", fmt.Errorf("parse template %s: %w", tmplPath, err)
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", tmplPath, err)
	}
	return sb.String(), nil
}

func renderHTMLLayout(root fs.FS, layoutPath string, content string, data any) (string, error) {
	t := htmltemplate.New("layout").Option("missingkey=error")
	partials, err := partialPaths(root, "html")
	if err != nil {
		return "", err
	}
	if len(partials) > 0 {
		for _, p := range partials {
			b, err := fs.ReadFile(root, p)
			if err != nil {
				return "", fmt.Errorf("read partial %s: %w", p, err)
			}
			if _, err := t.Parse(string(b)); err != nil {
				return "", fmt.Errorf("parse partial %s: %w", p, err)
			}
		}
	}
	b, err := fs.ReadFile(root, layoutPath)
	if err != nil {
		return "", fmt.Errorf("read layout %s: %w", layoutPath, err)
	}
	if _, err := t.Parse(string(b)); err != nil {
		return "", fmt.Errorf("parse layout %s: %w", layoutPath, err)
	}
	payload := map[string]any{
		//nolint:gosec // Content was rendered and escaped by html/template, safe to embed into layout.
		layoutContentKey: htmltemplate.HTML(content),
		layoutDataKey:    data,
	}
	var sb strings.Builder
	if err := t.Execute(&sb, payload); err != nil {
		return "", fmt.Errorf("execute layout %s: %w", layoutPath, err)
	}
	return sb.String(), nil
}

func renderTextTemplate(root fs.FS, tmplPath string, ext string, data any) (string, error) {
	t := texttemplate.New("content").Option("missingkey=error")
	partials, err := partialPaths(root, ext)
	if err != nil {
		return "", err
	}
	if len(partials) > 0 {
		for _, p := range partials {
			b, err := fs.ReadFile(root, p)
			if err != nil {
				return "", fmt.Errorf("read partial %s: %w", p, err)
			}
			if _, err := t.Parse(string(b)); err != nil {
				return "", fmt.Errorf("parse partial %s: %w", p, err)
			}
		}
	}
	b, err := fs.ReadFile(root, tmplPath)
	if err != nil {
		return "", err
	}
	if _, err := t.Parse(string(b)); err != nil {
		return "", fmt.Errorf("parse template %s: %w", tmplPath, err)
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", tmplPath, err)
	}
	return sb.String(), nil
}

func renderTextLayout(root fs.FS, layoutPath string, ext string, content string, data any) (string, error) {
	t := texttemplate.New("layout").Option("missingkey=error")
	partials, err := partialPaths(root, ext)
	if err != nil {
		return "", err
	}
	if len(partials) > 0 {
		for _, p := range partials {
			b, err := fs.ReadFile(root, p)
			if err != nil {
				return "", fmt.Errorf("read partial %s: %w", p, err)
			}
			if _, err := t.Parse(string(b)); err != nil {
				return "", fmt.Errorf("parse partial %s: %w", p, err)
			}
		}
	}
	b, err := fs.ReadFile(root, layoutPath)
	if err != nil {
		return "", fmt.Errorf("read layout %s: %w", layoutPath, err)
	}
	if _, err := t.Parse(string(b)); err != nil {
		return "", fmt.Errorf("parse layout %s: %w", layoutPath, err)
	}
	payload := map[string]any{
		layoutContentKey: content,
		layoutDataKey:    data,
	}
	var sb strings.Builder
	if err := t.Execute(&sb, payload); err != nil {
		return "", fmt.Errorf("execute layout %s: %w", layoutPath, err)
	}
	return sb.String(), nil
}

func exists(root fs.FS, targetPath string) (bool, error) {
	_, err := fs.Stat(root, targetPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat template %s: %w", targetPath, err)
	}
	return true, nil
}

func partialPaths(root fs.FS, ext string) ([]string, error) {
	entries, err := fs.ReadDir(root, "partials")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list partials: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "."+ext+".tmpl") {
			paths = append(paths, path.Join("partials", entry.Name()))
		}
	}
	return paths, nil
}
