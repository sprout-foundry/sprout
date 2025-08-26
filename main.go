/*
Package main provides the entry point for the Ledit CLI application.

Ledit is an AI-powered code editing and assistance tool designed to streamline
software development by leveraging Large Language Models (LLMs) to understand
your entire workspace, generate code, and orchestrate complex features.
*/
package main

import (
	"fmt"
	"github.com/alantheprice/ledit/cmd"
)

func main() {
	fmt.Println("Hello from Ledit!")
	cmd.Execute()
}
