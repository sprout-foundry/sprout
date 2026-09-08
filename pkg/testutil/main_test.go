//go:build !js

package testutil

import (
	"testing"

	"github.com/sprout-foundry/sprout/internal/testgit"
)

func TestMain(m *testing.M) { testgit.Main(m) }
