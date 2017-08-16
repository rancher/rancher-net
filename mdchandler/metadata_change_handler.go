package mdchandler

import (
	"fmt"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/rancher-net/backend"
)

const (
	metadataURLTemplate = "http://%v/2015-12-19"

	// DefaultMetadataAddress specifies the default value to use if nothing is specified
	DefaultMetadataAddress = "169.254.169.250"
)

var (
	changeCheckInterval = 2
)

// MetadataChangeHandler listens for version changes of metadata
// and triggers appropriate handlers in the current application
type MetadataChangeHandler struct {
	Backend backend.Backend
	mc      metadata.Client
}

// NewMetadataChangeHandler is used to create a OnChange
// handler for Meatadta
func NewMetadataChangeHandler(metadataAddress string, b backend.Backend) *MetadataChangeHandler {
	if metadataAddress == "" {
		metadataAddress = DefaultMetadataAddress
	}
	metadataURL := fmt.Sprintf(metadataURLTemplate, metadataAddress)
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		logrus.Errorf("couldn't create metadata client: %v", err)
		return nil
	}
	return &MetadataChangeHandler{
		Backend: b,
		mc:      mc,
	}
}

// OnChangeHandler is the actual callback function called when
// the metadata changes
func (mdch *MetadataChangeHandler) OnChangeHandler(version string) {
	logrus.Infof("Metadata OnChange received, version: %v", version)
	err := mdch.Backend.Reload()
	if err != nil {
		logrus.Errorf("Error reloading backend after receiving the db change: %v", err)
	} else {
		logrus.Debugf("Reload successful")
	}
}

// Start is used to begin the OnChange handling
func (mdch *MetadataChangeHandler) Start() error {
	logrus.Debugf("Starting the MetadataChangeHandler")
	mdch.mc.OnChange(changeCheckInterval, mdch.OnChangeHandler)

	return nil
}
