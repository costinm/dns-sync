package config

import (
	"context"
	"io/fs"
	"os"
	"sync"

	"sigs.k8s.io/yaml"
)

// Get will search for a config with the given name.
// The name should not include extension.
//
// Will look for .yaml, json - if not found will try an env variable.
// The name should not include "/".
func Get[T interface{}](ctx context.Context, name string) (*T, error) {
	var t T
	err := defaultCfg().GetConfig(ctx, name, &t)
	return &t, err

	return &t, nil
}

var (
	defaultCfg = sync.OnceValue[*FileConfig](func() *FileConfig {
		return &FileConfig{}
	})
)

type FileConfig struct {
	Base fs.ReadDirFS

}

func (fc *FileConfig) GetConfig(ctx context.Context, name string, t interface{}) error {

	cfgB, err := os.ReadFile(name + ".yaml")
	if err != nil {
		cfgS := os.Getenv(name)
		if cfgS != "" {
			cfgB = []byte(cfgS)
		}
	}

	if cfgB != nil {
		err = yaml.Unmarshal(cfgB, t)
		if err != nil {
			return err
		}
	}
	return nil
}
