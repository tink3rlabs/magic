package health

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/storage"
)

type HealthChecker struct {
	storage storage.StorageAdapter
}

func NewHealthChecker() *HealthChecker {
	s := storage.StorageAdapterFactory{}
	storageAdapter, err := s.GetInstance(storage.DEFAULT)
	if err != nil {
		logger.Fatal("failed to create HealthChecker instance", slog.Any("error", err.Error()))
	}
	return &HealthChecker{storage: storageAdapter}
}

func (h *HealthChecker) Check(checkStorage bool, dependencies []string) error {
	if checkStorage {
		err := h.storage.Ping()
		if err != nil {
			return fmt.Errorf("health check failure: storage check failed: %v", err)
		}
	}

	// TODO: consider supporting other methods and authN/Z
	for _, d := range dependencies {
		resp, err := http.Get(d)
		if err != nil {
			return fmt.Errorf("health check failure: request to dependency %s failed: %v", d, err)
		}
		if resp.StatusCode > 399 {
			return fmt.Errorf("health check failure: dependency %s returned response code %v", d, resp.StatusCode)
		}
	}
	return nil
}
