//go:build !js

package utils

import (
	"testing"

	"github.com/sprout-foundry/sprout/internal/testgit"
)

func TestMain(m *testing.M) { testgit.Main(m) }
