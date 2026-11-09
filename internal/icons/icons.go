// Package icons holds the app's SVG icons, from Lucide (ISC license, see
// svg/LICENSE), and the logos of coding agents, from LobeHub Icons (MIT
// license, see agents/LICENSE), which are their owners' trademarks.
package icons

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed svg/*.svg agents/*.svg
var files embed.FS

var (
	mu     sync.Mutex
	parsed = map[string]*ui.SVG{}
)

// Get returns the icon named after its file in svg/, without ".svg". It
// panics on a name that has no file, which is a bug in the app.
func Get(name string) *ui.SVG { return get("svg/" + name + ".svg") }

// Agent returns the logo of a coding agent, named after its file in
// agents/ as monitor.Agent's Icon names it, single-colored, to draw as
// an icon. It panics on a name that has no file.
func Agent(name string) *ui.SVG { return get("agents/" + name + ".svg") }

func get(file string) *ui.SVG {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := parsed[file]; ok {
		return s
	}
	data, err := files.ReadFile(file)
	if err != nil {
		panic("icons: no icon " + file)
	}
	s := ui.MustParseSVG(data)
	parsed[file] = s
	return s
}
