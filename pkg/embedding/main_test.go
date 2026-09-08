//go:build !js

package embedding

import (
	"testing"

	"github.com/sprout-foundry/sprout/internal/testgit"
)

func TestMain(m *testing.M) { testgit.Main(m) }
