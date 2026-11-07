package main

import "ofam.dev/ochazuke"

// declarePickers declares the tool's noun pickers (ochazuke.PickerDef): the
// catalog lists them and a host mounts them under the tool's prefix. Their
// rows come from each noun's list verb and a pick runs the noun's own open or
// get, as the derived pickers do; declaring them gives each an About line.
func declarePickers(r *ochazuke.Runtime) error {
	for _, d := range []ochazuke.PickerDef{
		{Name: "cmd", Noun: "cmd", About: "The saved commands; pick one to open it"},
		{Name: "run", Noun: "run", About: "The recorded runs of the saved commands; pick one to open it"},
	} {
		if err := r.AddPicker(d); err != nil {
			return err
		}
	}
	return nil
}
