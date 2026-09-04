package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	nethttp "net/http"
	"os"
	"path/filepath"
	"strings"
)

const defaultResponseCharset = "UTF-8"

// TemplateOptions defines the legacy template directory and layout contract.
type TemplateOptions struct {
	Directory       string
	Layout          string
	HTMLContentType string
}

type templateRenderer struct {
	templates  *template.Template
	options    TemplateOptions
	configured bool
}

var placeholderTemplateFunctions = template.FuncMap{
	"yield": func() (string, error) {
		return "", fmt.Errorf("yield called with no layout defined")
	},
	"current": func() (string, error) {
		return "", nil
	},
}

func newTemplateRenderer(options *TemplateOptions) (*templateRenderer, error) {
	base, err := template.New("transport").Funcs(placeholderTemplateFunctions).Parse("transport")
	if err != nil {
		return nil, fmt.Errorf("initialize HTTP templates: %w", err)
	}
	renderer := &templateRenderer{templates: base}
	if options == nil {
		return renderer, nil
	}

	renderer.options = *options
	renderer.configured = true
	if renderer.options.HTMLContentType == "" {
		renderer.options.HTMLContentType = "text/html"
	}

	err = filepath.WalkDir(renderer.options.Directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".tmpl" {
			return nil
		}
		relativePath, err := filepath.Rel(renderer.options.Directory, path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.ToSlash(relativePath), filepath.Ext(relativePath))
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := renderer.templates.New(name).Funcs(placeholderTemplateFunctions).Parse(string(contents)); err != nil {
			return fmt.Errorf("parse template %s: %w", path, err)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		// The legacy renderer allowed the service to start without resources and
		// returned an HTTP error only if an HTML route was requested.
		return renderer, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load HTTP templates from %s: %w", renderer.options.Directory, err)
	}
	return renderer, nil
}

type response struct {
	writer   nethttp.ResponseWriter
	request  *nethttp.Request
	renderer *templateRenderer
}

var _ Responder = (*response)(nil)

func (response *response) JSON(status int, value interface{}) {
	contents, err := json.Marshal(value)
	if err != nil {
		nethttp.Error(response.writer, err.Error(), nethttp.StatusInternalServerError)
		return
	}
	response.writer.Header().Set("Content-Type", "application/json; charset="+defaultResponseCharset)
	response.writer.WriteHeader(status)
	_, _ = response.writer.Write(contents)
}

func (response *response) HTML(status int, name string, value interface{}) {
	if response.renderer == nil || !response.renderer.configured {
		nethttp.Error(response.writer, "HTML renderer is not configured", nethttp.StatusInternalServerError)
		return
	}
	templates, err := response.renderer.templates.Clone()
	if err != nil {
		nethttp.Error(response.writer, err.Error(), nethttp.StatusInternalServerError)
		return
	}

	templateName := name
	if response.renderer.options.Layout != "" {
		templates = templates.Funcs(template.FuncMap{
			"yield": func() (template.HTML, error) {
				var body bytes.Buffer
				if err := templates.ExecuteTemplate(&body, name, value); err != nil {
					return "", err
				}
				return template.HTML(body.String()), nil
			},
			"current": func() (string, error) {
				return name, nil
			},
		})
		templateName = response.renderer.options.Layout
	}

	var contents bytes.Buffer
	if err := templates.ExecuteTemplate(&contents, templateName, value); err != nil {
		nethttp.Error(response.writer, err.Error(), nethttp.StatusInternalServerError)
		return
	}
	response.writer.Header().Set("Content-Type", response.renderer.options.HTMLContentType+"; charset="+defaultResponseCharset)
	response.writer.WriteHeader(status)
	_, _ = response.writer.Write(contents.Bytes())
}

func (response *response) Redirect(location string, status ...int) {
	code := nethttp.StatusFound
	if len(status) == 1 {
		code = status[0]
	}
	nethttp.Redirect(response.writer, response.request, location, code)
}
