// Package icons holds the app's SVG icons, from Lucide (ISC license, see
// svg/LICENSE).
package icons

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed svg/*.svg
var files embed.FS

var (
	mu     sync.Mutex
	parsed = map[string]*ui.SVG{}
)

// Get returns the icon named after its file in svg/, without ".svg". It
// panics on a name that has no file, which is a bug in the app.
func Get(name string) *ui.SVG {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := parsed[name]; ok {
		return s
	}
	data, err := files.ReadFile("svg/" + name + ".svg")
	if err != nil {
		panic("icons: no icon " + name)
	}
	s := ui.MustParseSVG(data)
	parsed[name] = s
	return s
}
