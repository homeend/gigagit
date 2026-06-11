package main

import (
	"fmt"

	"github.com/gigagit/gg/internal/buildinfo"
)

func main() {
	fmt.Println(buildinfo.String())
}
