package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// The platforms releases are built for: the modules an app links can
// differ between them (golang.org/x/sys/windows and so on).
var platforms = []string{"darwin/arm64", "linux/amd64", "windows/amd64"}

//go:embed notices/ghostty.txt
var ghosttyLicense string

// licenseChoices says which license the app takes a module under when the
// module offers a choice, and adds that license's text from the module.
var licenseChoices = map[string]struct{ note, file string }{
	// FreeType License or GPL 2+; the GPL doesn't fit the app's license.
	// The FreeType License asks for this credit in the documentation.
	"github.com/golang/freetype": {
		note: "Finallyshell uses Freetype-Go under the FreeType License. " +
			"Portions of this software are copyright (c) 2017 The FreeType Project (www.freetype.org). All rights reserved.",
		file: "licenses/ftl.txt",
	},
}

// repoNotices are the licenses of third-party files kept in this
// repository and embedded in the app.
var repoNotices = []struct{ title, path string }{
	{"Lucide icons (https://lucide.dev)", "internal/icons/svg/LICENSE"},
}

// Module is a Go module the app links.
type Module struct{ Path, Version, Dir string }

// LinkedModules lists the modules, other than the main one, of the
// packages that the package in dir links on any release platform.
func LinkedModules(dir string) ([]Module, error) {
	seen := map[string]Module{}
	for _, p := range platforms {
		goos, goarch, _ := strings.Cut(p, "/")
		cmd := exec.Command("go", "list", "-deps", "-f",
			`{{with .Module}}{{if not .Main}}{{with .Replace}}{{.Path}}	{{.Version}}	{{.Dir}}{{else}}{{.Path}}	{{.Version}}	{{.Dir}}{{end}}{{end}}{{end}}`, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list for %s: %v", p, err)
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			f := strings.Split(sc.Text(), "\t")
			if len(f) == 3 && f[0] != "" {
				seen[f[0]] = Module{f[0], f[1], f[2]}
			}
		}
	}
	mods := make([]Module, 0, len(seen))
	for _, m := range seen {
		mods = append(mods, m)
	}
	slices.SortFunc(mods, func(a, b Module) int { return strings.Compare(a.Path, b.Path) })
	return mods, nil
}

// licenseFile reports whether name is a license or notice file.
func licenseFile(name string) bool {
	n := strings.ToUpper(name)
	for _, p := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "THIRDPARTYNOTICES"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// moduleLicenses reads the license and notice files at the root of a
// module's directory.
func moduleLicenses(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var texts []string
	for _, e := range entries {
		if e.Type().IsRegular() && licenseFile(e.Name()) {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			texts = append(texts, string(b))
		}
	}
	return texts, nil
}

// vsixLicenses reads the license and notice files of a VS Code extension
// package, in its extension folder.
func vsixLicenses(path string) ([]string, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	var texts []string
	for _, f := range z.File {
		dir, name := filepath.Split(f.Name)
		if dir != "extension/" || !licenseFile(name) {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		texts = append(texts, string(b))
	}
	return texts, nil
}

// ThirdPartyLicenses writes the notices of what the app in dir ships:
// Go and its standard library, each linked module, libghostty-vt, which
// MyGo's terminal plugin bundles as a binary, the files of repoNotices,
// and the VS Code icon themes in internal/fileicons/bundled. A module without a license file is an
// error, so that a new dependency can't ship unnoticed.
func ThirdPartyLicenses(dir string, w io.Writer) error {
	section := func(title string, texts ...string) {
		fmt.Fprintf(w, "%s\n%s\n\n", title, strings.Repeat("=", len(title)))
		for _, t := range texts {
			fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(t))
		}
	}
	fmt.Fprint(w, "Finallyshell includes the following third-party software.\n\n")

	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env GOROOT: %v", err)
	}
	goLicense, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "LICENSE"))
	if err != nil {
		return err
	}
	section("Go (https://go.dev)", string(goLicense))

	mods, err := LinkedModules(dir)
	if err != nil {
		return err
	}
	for _, m := range mods {
		texts, err := moduleLicenses(m.Dir)
		if err != nil {
			return fmt.Errorf("%s: %v", m.Path, err)
		}
		if len(texts) == 0 {
			return fmt.Errorf("%s %s has no license file in %s", m.Path, m.Version, m.Dir)
		}
		if c, ok := licenseChoices[m.Path]; ok {
			b, err := os.ReadFile(filepath.Join(m.Dir, c.file))
			if err != nil {
				return fmt.Errorf("%s: %v", m.Path, err)
			}
			texts = append(texts, c.note, string(b))
		}
		section(m.Path+" "+m.Version, texts...)
	}

	section("libghostty-vt (https://github.com/ghostty-org/ghostty)", ghosttyLicense)

	for _, n := range repoNotices {
		b, err := os.ReadFile(filepath.Join(dir, n.path))
		if err != nil {
			return err
		}
		section(n.title, string(b))
	}

	vsix, err := filepath.Glob(filepath.Join(dir, "internal", "fileicons", "bundled", "*.vsix"))
	if err != nil {
		return err
	}
	for _, p := range vsix {
		texts, err := vsixLicenses(p)
		if err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		if len(texts) == 0 {
			return fmt.Errorf("%s has no license file", p)
		}
		section("VS Code icon theme "+strings.TrimSuffix(filepath.Base(p), ".vsix"), texts...)
	}
	return nil
}
