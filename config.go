package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"
)

// configFS holds the files the images are built from. They are embedded at
// build time, so to change them, edit config/ and rebuild claude-box. Files
// ending in .tmpl are rendered with templateData (e.g. {{.Subnet}}) and used
// without the suffix.
//
//go:embed config
var configFS embed.FS

// templateData holds the values the .tmpl files can use.
var templateData = struct{ Subnet string }{subnet}

// loadFiles returns the embedded files by name, with templates rendered.
func loadFiles() (map[string][]byte, error) {
	entries, err := configFS.ReadDir("config")
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte)
	for _, e := range entries {
		data, err := configFS.ReadFile("config/" + e.Name())
		if err != nil {
			return nil, err
		}
		name := e.Name()
		if base, ok := strings.CutSuffix(name, ".tmpl"); ok {
			name = base
			if data, err = render(e.Name(), data); err != nil {
				return nil, err
			}
		}
		files[name] = data
	}
	return files, nil
}

func render(name string, text []byte) ([]byte, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(string(text))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, templateData); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// filesHash returns a short hash of the named files, used to tell whether an
// image (or the proxy container) was built from the current files.
func filesHash(files map[string][]byte, names []string) string {
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s %d\n", name, len(files[name]))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
