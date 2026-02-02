package main

import (
	"os"

	"easychat/internal/platform/server"
)

var runServer = server.Run
var exitProcess = os.Exit

func main() {
	if err := runServer(); err != nil {
		exitProcess(1)
	}
}
