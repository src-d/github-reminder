package main

import (
	"net/http"

	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"

	"github.com/src-d/github-reminder/handler"
)

func main() {
	var config struct {
		Address    string `default:":8080"`
		AppID      int    `required:"true" split_words:"true"`
		PrivateKey string `split_words:"true"`
		Secret     string
		Verbose    bool
	}
	envconfig.MustProcess("github_reminder", &config)

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
