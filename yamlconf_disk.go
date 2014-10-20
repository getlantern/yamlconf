package yamlconf

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"

	"gopkg.in/getlantern/yaml.v1"
)

func (m *Manager) loadFromDisk() error {
	_, err := m.reloadFromDisk()
	return err
}

func (m *Manager) reloadFromDisk() (bool, error) {
	cfg := m.EmptyConfig()

	fileInfo, err := os.Stat(m.FilePath)
	if err != nil {
		return false, fmt.Errorf("Unable to stat config file %s: %s", m.FilePath, err)
	}
	if m.fileInfo == fileInfo {
		log.Trace("Config unchanged on disk")
		return false, nil
	}
	bytes, err := ioutil.ReadFile(m.FilePath)
	if err != nil {
		return false, fmt.Errorf("Error reading config from %s: %s", m.FilePath, err)
	}
	err = yaml.Unmarshal(bytes, cfg)
	if err != nil {
		return false, fmt.Errorf("Error unmarshaling config yaml from %s: %s", m.FilePath, err)
	}

	if m.cfg != nil && m.cfg.GetVersion() != cfg.GetVersion() {
		log.Trace("Version mismatch on disk, overwriting what's on disk with current version")
		m.writeToDisk(m.cfg)
		return false, fmt.Errorf("Version of config on disk did not match expected. Expected %d, found %d", m.cfg.GetVersion(), cfg.GetVersion())
	}

	if reflect.DeepEqual(m.cfg, cfg) {
		log.Trace("Config on disk is same as in memory, ignoring")
		return false, nil
	}

	m.cfg = cfg
	m.fileInfo = fileInfo

	return true, nil
}

func (m *Manager) saveToDiskAndUpdate(updated Config) (bool, error) {
	log.Trace("Applying defaults before saving")
	updated.ApplyDefaults()

	log.Trace("Remembering current version")
	currentVersion := 0
	if m.cfg != nil {
		currentVersion = m.cfg.GetVersion()
		log.Trace("Temporarily set version to 0 to prepare for comparison")
		m.cfg.SetVersion(0)
	}

	log.Trace("Compare config without version")
	updated.SetVersion(0)
	if reflect.DeepEqual(m.cfg, updated) {
		log.Trace("Configuration unchanged, do nothing")
		m.cfg.SetVersion(currentVersion)
		return false, nil
	}

	log.Trace("Increment version")
	updated.SetVersion(currentVersion + 1)

	log.Trace("Save updated")
	err := m.writeToDisk(updated)
	if err != nil {
		return false, err
	}

	log.Trace("Point to updated")
	m.cfg = updated
	return true, nil
}

func (m *Manager) writeToDisk(cfg Config) error {
	bytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("Unable to marshal config yaml: %s", err)
	}
	err = ioutil.WriteFile(m.FilePath, bytes, 0644)
	if err != nil {
		return fmt.Errorf("Unable to write config yaml to file %s: %s", m.FilePath, err)
	}
	m.fileInfo, err = os.Stat(m.FilePath)
	if err != nil {
		return fmt.Errorf("Unable to stat file %s: %s", m.FilePath, err)
	}
	return nil
}

// HasChangedOnDisk checks whether Config has changed on disk
func (m *Manager) hasChangedOnDisk() bool {
	nextFileInfo, err := os.Stat(m.fileInfo.Name())
	if err != nil {
		return false
	}
	hasChanged := nextFileInfo.Size() != m.fileInfo.Size() || nextFileInfo.ModTime() != m.fileInfo.ModTime()
	return hasChanged
}
