//go:build !js

package cmd

import (
	"github.com/sprout-foundry/sprout/pkg/service"
)

func init() {
	rootCmd.AddCommand(service.ServiceCmd)
}
