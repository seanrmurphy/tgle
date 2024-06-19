package main

import (
	"os"
	"path/filepath"

	"github.com/pterm/pterm"

	"github.com/adrg/xdg"
	"github.com/go-faster/errors"
	"github.com/pelletier/go-toml/v2"
)

// ApplicationName is used to determine where files will be stored within the
// XDG directory system. Currently, it is assumed that this is unique, ie that
// another application call "tgle" does not already exist."
var ApplicationName = "tgle"

// Config contains the c configuration for the application.
type Config struct {
	TgleStateDirectory  string
	TelegramPhoneNumber string
	TelegramAppID       string
	TelegramAppHash     string
}

func readConfigFile(configFilePath string) (cfg *Config, err error) {
	configFilePath = filepath.Clean(configFilePath)
	fileContent, err := os.ReadFile(configFilePath)
	if err != nil {
		return cfg, errors.Wrap(err, "unable to read config file")
	}
	cfg = &Config{}
	err = toml.Unmarshal(fileContent, &cfg)
	if err != nil {
		return cfg, errors.Wrap(err, "unable to parse config file")
	}
	return cfg, nil
}

func writeConfig(configFilePath string, cfg Config) error {
	configFilePath = filepath.Clean(configFilePath)
	file, err := os.Create(configFilePath)
	if err != nil {
		return errors.Wrap(err, "unable to create config file")
	}
	defer file.Close()
	err = toml.NewEncoder(file).Encode(cfg)
	if err != nil {
		return errors.Wrap(err, "unable to write config file")
	}
	return nil
}

// readConfig reads the config file and returns it in a config struct.
// in the initial version, we check if the config file exists in XDG_CONFIG_HOME;
// if so, we read it. If not, we create a new one based on user input.
// We don't support user variables at this time.
func readConfig() (cfg *Config, err error) {

	// check if a config file exists in XDG_CONFIG_HOME
	configFilePath, err := xdg.ConfigFile(ApplicationName + "/config.toml")
	if err != nil {
		lg.Fatal(err.Error())
	}

	if _, err := os.Stat(configFilePath); err == nil {
		return readConfigFile(configFilePath)
	} else if errors.Is(err, os.ErrNotExist) {
		pterm.Info.Print("No configuration exists - need to create one...\n")
		pterm.Println()

		phoneNumber, _ := pterm.DefaultInteractiveTextInput.Show("enter telegram phone number - (use format +1234567890)")

		appID, _ := pterm.DefaultInteractiveTextInput.Show("enter telegram app id")

		appHash, _ := pterm.DefaultInteractiveTextInput.Show("enter telegram app hash")

		TgleStateDirectory := xdg.StateHome + "/" + ApplicationName

		cfg = &Config{
			TgleStateDirectory:  TgleStateDirectory,
			TelegramPhoneNumber: phoneNumber,
			TelegramAppID:       appID,
			TelegramAppHash:     appHash,
		}
		err := writeConfig(configFilePath, *cfg)
		if err != nil {
			lg.Sugar().Error("error writing config file: %v", err)
			return nil, err
		}
		return cfg, nil
	}
	return
}
