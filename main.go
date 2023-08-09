package main

import (
	. "anserem/cmd/anserem"
	"context"
	"fmt"
	"log"
	"os"
)

// main is the entry point and starts the application process.
func main() {
	log.Println("starting application")
	if err := Command().Run(context.Background(), os.Args); err != nil {
		_, _ = fmt.Fprint(os.Stderr, err)
	}
}
