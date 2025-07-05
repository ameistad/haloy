package db

import (
	"encoding/json"
	"time"

	"github.com/ameistad/haloy/internal/config"
)

type Deployment struct {
	ID             string          `db:"id" json:"id"`
	AppName        string          `db:"app_name" json:"app_name"`
	AppConfig      json.RawMessage `db:"app_config" json:"app_config"`
	ImageTag       string          `db:"image_tag" json:"image_tag"`
	RolledBackFrom *string         `db:"rolled_back_from" json:"rolled_back_from,omitempty"`
}

func (d *Deployment) GetAppConfig() (config.AppConfig, error) {
	var cfg config.AppConfig
	err := json.Unmarshal(d.AppConfig, &cfg)
	return cfg, err
}

func (d *Deployment) SetAppConfig(cfg config.AppConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	d.AppConfig = data
	return nil
}

func (d *Deployment) IsRollback() bool {
	return d.RolledBackFrom != nil
}

func (d *Deployment) GetTimestamp() (time.Time, error) {
	return time.Parse("20060102-150405", d.ID)
}

// Constructor helpers - keep in models.go
func NewDeployment(id, appName, imageTag string, cfg config.AppConfig) (*Deployment, error) {
	deployment := &Deployment{
		ID:       id,
		AppName:  appName,
		ImageTag: imageTag,
	}

	if err := deployment.SetAppConfig(cfg); err != nil {
		return nil, err
	}

	return deployment, nil
}

func NewRollbackDeployment(id, appName, imageTag string, cfg config.AppConfig, rolledBackFrom string) (*Deployment, error) {
	deployment, err := NewDeployment(id, appName, imageTag, cfg)
	if err != nil {
		return nil, err
	}

	deployment.RolledBackFrom = &rolledBackFrom
	return deployment, nil
}
