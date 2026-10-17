package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCheckWorkdir(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Errorf("TestCheckWorkdir failed to get HOME")
	}
	for _, tc := range []struct {
		workdir string
		ok      bool
	}{
		{"/", false},
		{"/home", false},
		{home, false},
		{home + "/", false},
		{home + "/src/project", true},
		{"/home/ryanx", true},
		{"/srv/project", true},
	} {
		err := checkWorkdir(config{Home: home, Workdir: tc.workdir})
		if (err == nil) != tc.ok {
			t.Errorf("checkWorkdir(%q) = %v, want ok=%v", tc.workdir, err, tc.ok)
		}
	}
}

func TestLoadFiles(t *testing.T) {
	files, err := loadFiles()
	if err != nil {
		t.Fatal(err)
	}

	// Every file an image is built from must be embedded.
	for _, img := range []image{proxyImage, boxImage} {
		for _, name := range img.Files {
			if _, ok := files[name]; !ok {
				t.Errorf("%s: %s is not embedded", img.Name, name)
			}
		}
	}
	if !slices.Contains(strings.Split(string(files["tinyproxy.conf"]), "\n"), "Allow "+subnet) {
		t.Errorf("tinyproxy.conf lacks the subnet:\n%s", files["tinyproxy.conf"])
	}

	// Changing a file changes the hash of the images built from it, only.
	proxy, box := proxyImage.Files, boxImage.Files
	before := [2]string{filesHash(files, proxy), filesHash(files, box)}
	files["allowlist"] = append(files["allowlist"], "^example\\.com:443$\n"...)
	after := [2]string{filesHash(files, proxy), filesHash(files, box)}
	if before[0] == after[0] || before[1] != after[1] {
		t.Errorf("after editing the allowlist: proxy hash %s -> %s, box hash %s -> %s; want only the proxy's to change",
			before[0], after[0], before[1], after[1])
	}
}
