package main

import (
	fairplex "github.com/eu90h/fairplex/pkg"
)

func main() {
	fp := fairplex.Fairplex{}
	fp.RequestsPerMinute = 100
	engine := fp.SetupRouter()
	engine.Run("0.0.0.0:8118")
}