package core

import (
	"testing"

	"github.com/admin8800/s-ui/logger"
	"github.com/op/go-logging"
)

func TestCoreRejectsInvalidConfig(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	core := NewCore()
	invalid := []byte(`{"route":`)

	if err := core.ValidateConfig(invalid); err == nil {
		t.Fatal("ValidateConfig accepted invalid JSON")
	}
	if err := core.Start(invalid); err == nil {
		t.Fatal("Start accepted invalid JSON")
	}
	if core.IsRunning() {
		t.Fatal("core is running after invalid config")
	}
}
