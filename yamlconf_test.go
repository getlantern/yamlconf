package yamlconf

import (
	"bytes"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/getlantern/testify/assert"
	"gopkg.in/getlantern/yaml.v1"
)

const (
	FIXED_I = 55
)

var (
	pollInterval = 100 * time.Millisecond
)

type TestCfg struct {
	Version int
	N       *Nested
}

type Nested struct {
	S string
	I int
}

func (c *TestCfg) GetVersion() int {
	return c.Version
}

func (c *TestCfg) SetVersion(version int) {
	c.Version = version
}

func (c *TestCfg) ApplyDefaults() {
	if c.N == nil {
		c.N = &Nested{}
	}
	if c.N.I == 0 {
		c.N.I = FIXED_I
	}
}

func TestNormal(t *testing.T) {
	file, err := ioutil.TempFile("", "yamlconf_test_")
	if err != nil {
		t.Fatalf("Unable to create temp file: %s", err)
	}
	defer os.Remove(file.Name())

	m := &Manager{
		EmptyConfig: func() Config {
			return &TestCfg{}
		},
		FilePath:         file.Name(),
		FilePollInterval: pollInterval,
	}

	err = m.Start()
	if err != nil {
		t.Fatalf("Unable to start manager: %s", err)
	}

	assertSavedConfigEquals(t, file, &TestCfg{
		Version: 1,
		N: &Nested{
			I: FIXED_I,
		},
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		// Push updates

		// Push update to file
		saveConfig(t, file, &TestCfg{
			Version: 1,
			N: &Nested{
				S: "3",
				I: 3,
			},
		})

		// Wait for file update to get picked up
		time.Sleep(pollInterval * 2)

		// Perform update programmatically
		err := m.Update(func(cfg Config) error {
			tc := cfg.(*TestCfg)
			tc.N.S = "4"
			tc.N.I = 4
			return nil
		})
		if err != nil {
			t.Fatalf("Unable to issue first update: %s", err)
		}

		wg.Done()
	}()

	updated := m.Next()
	assert.Equal(t, &TestCfg{
		Version: 1,
		N: &Nested{
			I: 55,
		},
	}, updated, "Config from initial load should contain correct data, including automatic version and default I")

	updated = m.Next()
	assert.Equal(t, &TestCfg{
		Version: 1,
		N: &Nested{
			S: "3",
			I: 3,
		},
	}, updated, "Config from updated file should contain correct data")

	updated = m.Next()
	assert.Equal(t, &TestCfg{
		Version: 2,
		N: &Nested{
			S: "4",
			I: 4,
		},
	}, updated, "Config from programmatic update should contain correct data, including updated version")

	wg.Wait()
}

func assertSavedConfigEquals(t *testing.T, file *os.File, expected *TestCfg) {
	b, err := yaml.Marshal(expected)
	if err != nil {
		t.Fatalf("Unable to marshal expected to yaml: %s", err)
	}
	bod, err := ioutil.ReadFile(file.Name())
	if err != nil {
		t.Errorf("Unable to read config from disk: %s", err)
	}
	if !bytes.Equal(b, bod) {
		t.Errorf("Saved config doesn't equal expected.\n---- Expected ----\n%s\n\n---- On Disk ----:\n%s\n\n", string(b), string(bod))
	}
}

func saveConfig(t *testing.T, file *os.File, updated *TestCfg) {
	b, err := yaml.Marshal(updated)
	if err != nil {
		t.Fatalf("Unable to marshal updated to yaml: %s", err)
	}
	err = ioutil.WriteFile(file.Name(), b, 0644)
	if err != nil {
		t.Fatalf("Unable to save test config: %s", err)
	}
}
