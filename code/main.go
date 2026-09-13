package main

import "os"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "show" {
		os.Exit(runShow(os.Args[2:]))
	}
	runLegacy()
}
