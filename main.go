package main

import (
	"context"
	"trpcagent/bootstrap"
)

func main() {
	bootstrap.Boot(context.Background(), "HyperBoot")
}
