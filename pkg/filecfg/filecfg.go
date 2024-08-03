package filecfg

import (
	"os"

	"sigs.k8s.io/yaml"
)

func GetConfig(name string, dest interface{}) error {

	cfgB, err := os.ReadFile(name)
	if err != nil {
		cfgS := os.Getenv("GOOGLE_DNS_CONFIG")
		if cfgS != "" {
			cfgB = []byte(cfgS)
		}
	}

	if cfgB != nil {
		err = yaml.Unmarshal(cfgB, dest)
		if err != nil {
			return err
		}
	}
	return nil
}
