package help

import (
	"fmt"
	"strings"
	"testing"

	"github.com/blainemoser/TrySql/utils"
)

func TestGet(t *testing.T) {
	result := Get([]string{"help"})
	var errs []error
	for key := range info {
		if !strings.Contains(result, key) {
			errs = append(errs, fmt.Errorf("expected help output to contain '%s'", key))
		}
	}
	err := utils.GetErrors(errs)
	if err != nil {
		t.Error(err)
	}
}

func TestVersion(t *testing.T) {
	result := Get([]string{"help", "version"})
	var errs []error
	for key := range info {
		if key == "docker-version" {
			continue
		}
		if strings.Contains(result, key) {
			errs = append(errs, fmt.Errorf("expected help output not to contain '%s'", key))
		}
	}
	err := utils.GetErrors(errs)
	if err != nil {
		t.Error(err)
	}
}

func TestUnknown(t *testing.T) {
	result := Get([]string{"help", "help", "h", "nothing", "version", "history"})
	var errs []error
	if strings.Contains(result, "help") {
		errs = append(errs, fmt.Errorf("did not expect output to contain help information"))
	}
	if !strings.Contains(result, "No command 'nothing'") {
		errs = append(errs, fmt.Errorf("expected output to contain \"No command 'nothing'\""))
	}
	for key := range info {
		if key == "docker-version" || key == "history" {
			continue
		}
		if strings.Contains(result, key) {
			errs = append(errs, fmt.Errorf("expected help output not to contain '%s'", key))
		}
	}
	err := utils.GetErrors(errs)
	if err != nil {
		t.Error(err)
	}
}
