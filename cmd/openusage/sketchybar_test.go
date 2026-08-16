package main

import "testing"

func TestSketchybarCommandHasIntegrationControls(t *testing.T) {
	cmd := newSketchybarCommand()
	want := map[string]bool{"install": false, "uninstall": false, "doctor": false, "presets": false}
	for _, child := range cmd.Commands() {
		if _, ok := want[child.Name()]; ok {
			want[child.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing sketchybar %s subcommand", name)
		}
	}
}
