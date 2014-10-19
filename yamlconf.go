package yamlconf

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/getlantern/golog"
	"gopkg.in/getlantern/deepcopy.v1"
)

var (
	log = golog.LoggerFor("yamlconf")
)

type Config interface {
	GetVersion() int

	SetVersion(version int)

	ApplyDefaults()
}

type Manager struct {
	// EmptyConfig: required, factor for new empty Configs
	EmptyConfig func() Config

	// FilePath: required, path to the config file on disk
	FilePath string

	// FilePollInterval: how frequently to poll the file for changes, defaults
	// to 1 second
	FilePollInterval time.Duration

	// HttpURL: optional, if specified, config will be fetched from this HTTP
	// URL. This mechanism supports ETags to avoid processing unchanged
	// configuration data.
	HttpURL string

	// HttpCert: optional, if specified, the TLS connection to the HttpURL will
	// be verified against this certificate.
	HttpCert string

	// HttpPollInterval: how frequently to poll the HttpURL for changes,
	// defaults to 1 minute.
	HttpPollInterval time.Duration

	// RandomizeHttpPollInterval: if true, the actual HttpPollInterval will be
	// randomized from poll to poll.
	RandomizeHttpPollInterval bool

	cfg       Config
	fileInfo  os.FileInfo
	deltasCh  chan *delta
	nextCfgCh chan Config
}

// delta is a change to the configuration
type delta struct {
	deltaFn func(cfg Config) error
	errCh   chan error
}

func (m *Manager) Next() Config {
	return <-m.nextCfgCh
}

func (m *Manager) Update(deltaFn func(cfg Config) error) error {
	errCh := make(chan error)
	m.deltasCh <- &delta{deltaFn, errCh}
	return <-errCh
}

func (m *Manager) Start() error {
	if m.EmptyConfig == nil {
		return fmt.Errorf("EmptyConfig must be specified")
	}
	if m.FilePath == "" {
		return fmt.Errorf("FilePath must be specified")
	}
	if m.FilePollInterval == 0 {
		m.FilePollInterval = 1 * time.Second
	}
	if m.HttpPollInterval == 0 {
		m.HttpPollInterval = 1 * time.Minute
	}
	m.deltasCh = make(chan *delta)
	m.nextCfgCh = make(chan Config)

	err := m.loadFromDisk()
	if err != nil {
		// Problem reading config, assume that we need to save a new one
		cfg := m.EmptyConfig()
		cfg.ApplyDefaults()
		_, err = m.saveToDiskAndUpdate(cfg)
		if err != nil {
			return err
		}
	} else {
		// Always save whatever we loaded, which will cause defaults to be
		// applied and formatting to be made consistent
		copied, err := m.copy(m.cfg)
		if err == nil {
			_, err = m.saveToDiskAndUpdate(copied)
		}
		if err != nil {
			return fmt.Errorf("Unable to perform initial update of config on disk: %s", err)
		}
	}

	go func() {
		m.nextCfgCh <- m.cfg
		m.processUpdates()
	}()

	return nil
}

func (m *Manager) processUpdates() {
	nextHttp := m.nextHttpPoll()
	for {
		log.Trace("Waiting for next update")
		timeToNextHttp := nextHttp.Sub(time.Now())
		changed := false
		select {
		case delta := <-m.deltasCh:
			log.Trace("Apply delta")
			updated, err := m.copy(m.cfg)
			err = delta.deltaFn(updated)
			if err != nil {
				delta.errCh <- err
				continue
			}
			changed, err = m.saveToDiskAndUpdate(updated)
			delta.errCh <- err
			if err != nil {
				continue
			}
		case <-time.After(m.FilePollInterval):
			log.Trace("Read update from disk")
			var err error
			changed, err = m.reloadFromDisk()
			if err != nil {
				log.Errorf("Unable to read updated config from disk: %s", err)
				continue
			}
		case <-time.After(timeToNextHttp):
			if m.HttpURL != "" {
				log.Trace("Check for remote updates")
				// updated, err = fetchCloudConfig(cfg)
				// if updated == nil && err == nil {
				// 	log.Debugf("Configuration unchanged in cloud at: %s", cfg.CloudConfig)
				// }
			}
			nextHttp = m.nextHttpPoll()
		}

		if changed {
			log.Trace("Publish changed config")
			m.nextCfgCh <- m.cfg
		}
	}
}

func (m *Manager) nextHttpPoll() time.Time {
	sleepTime := m.HttpPollInterval
	if m.RandomizeHttpPollInterval {
		sleepTime = time.Duration((sleepTime.Nanoseconds() / 2) + rand.Int63n(sleepTime.Nanoseconds()))
	}
	return time.Now().Add(time.Duration(sleepTime))
}

// func (m *Manager) fetchHttpConfig(ch *configHolder) error {
// 	log.Debugf("Fetching HTTP config from: %s", m.HttpURL)
// 	// Try it unproxied first
// 	bytes, err := doFetchCloudConfig(cfg, "")
// 	if err != nil && cfg.IsDownstream() {
// 		// If that failed, try it proxied
// 		bytes, err = doFetchCloudConfig(cfg, cfg.Addr)
// 	}
// 	if err != nil {
// 		return nil, fmt.Errorf("Unable to read yaml from %s: %s", cfg.CloudConfig, err)
// 	}
// 	if bytes == nil {
// 		return nil, nil
// 	}
// 	log.Debugf("Merging cloud configuration")
// 	return cfg.UpdatedFrom(bytes)
// }

// func (m *Manager) doFetchHttpConfig(cfg *config.Config, proxyAddr string) ([]byte, error) {
// 	client, err := util.HTTPClient(cfg.CloudConfigCA, proxyAddr)
// 	if err != nil {
// 		return nil, fmt.Errorf("Unable to initialize HTTP client: %s", err)
// 	}
// 	log.Debugf("Checking for cloud configuration at: %s", cfg.CloudConfig)
// 	req, err := http.NewRequest("GET", cfg.CloudConfig, nil)
// 	if err != nil {
// 		return nil, fmt.Errorf("Unable to construct request for cloud config at %s: %s", cfg.CloudConfig, err)
// 	}
// 	if lastCloudConfigETag != "" {
// 		// Don't bother fetching if unchanged
// 		req.Header.Set(IF_NONE_MATCH, lastCloudConfigETag)
// 	}
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return nil, fmt.Errorf("Unable to fetch cloud config at %s: %s", cfg.CloudConfig, err)
// 	}
// 	defer resp.Body.Close()
// 	if resp.StatusCode == 304 {
// 		return nil, nil
// 	} else if resp.StatusCode != 200 {
// 		return nil, fmt.Errorf("Unexpected response status: %d", resp.StatusCode)
// 	}
// 	lastCloudConfigETag = resp.Header.Get(ETAG)
// 	return ioutil.ReadAll(resp.Body)
// }

func (m *Manager) copy(orig Config) (copied Config, err error) {
	copied = m.EmptyConfig()
	err = deepcopy.Copy(copied, orig)
	return
}
