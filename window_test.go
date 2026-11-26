package main

import (
	"testing"

	"finallyshell/internal/app"

	"github.com/egoist/mygo"
)

func TestSecondaryWindowOptions(t *testing.T) {
	parent := new(mygo.Window)
	bounds := mygo.Rectangle{X: 100, Y: 200, Width: 1280, Height: 820}
	spec := app.WindowSpec{Title: func() string { return "Editor" }, Width: 1000, Height: 700}
	owned := secondaryWindowOptions(spec, parent, bounds)
	if owned.Parent != parent || !owned.DisableMinimize || owned.Modal || owned.AlwaysOnTop {
		t.Fatalf("owned editor options %+v", owned)
	}
	spec.Independent = true
	independent := secondaryWindowOptions(spec, parent, bounds)
	if independent.Parent != nil || independent.DisableMinimize || independent.Modal || independent.AlwaysOnTop {
		t.Fatalf("independent editor options %+v", independent)
	}
	if independent.X != 240 || independent.Y != 260 || independent.X != owned.X || independent.Y != owned.Y {
		t.Errorf("owned position (%d, %d), independent position (%d, %d)", owned.X, owned.Y, independent.X, independent.Y)
	}
	spec.Independent, spec.Modal = false, true
	modal := secondaryWindowOptions(spec, parent, bounds)
	if modal.Parent != parent || !modal.Modal || !modal.DisableMinimize {
		t.Errorf("modal dialog options %+v", modal)
	}
}
