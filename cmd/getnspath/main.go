package main

import (
	"context"
	"fmt"
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/cri"
)

func main() {
	if len(os.Args) <= 1 {
		panic("expected a container id")
	}
	containerID := os.Args[1]

	cri, err := cri.New()
	if err != nil {
		panic(err)
	}
	info, err := cri.GetContainerInfo(context.TODO(), containerID)
	if err != nil {
		panic(err)
	}

	fmt.Println(*info)
}
