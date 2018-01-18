package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"

	"github.com/src-d/github-reminder/handler"
)

func main() {
	var config struct {
		Address    string `default:":8080" desc:"address where the server will listen to"`
		AppID      int    `required:"true" split_words:"true" desc:"GitHub application id"`
		PrivateKey string `split_words:"true" desc:"contents of the GitHub application private key"`
		Secret     string `desc:"GitHub application's secret value"`
		Verbose    bool
	}
	if err := envconfig.Process("github_reminder", &config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		envconfig.Usage("github_reminder", &config)
		os.Exit(1)
	}

	if config.Verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	h, err := handler.New(config.AppID, []byte(config.PrivateKey), []byte(config.Secret), nil)
	if err != nil {
		logrus.Fatal(err)
	}

	logrus.Infof("github-reminder listening on %s", config.Address)
	logrus.Fatal(http.ListenAndServe(config.Address, h))
}
