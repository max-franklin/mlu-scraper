package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

type Config struct {
	ScrapeBaseUrl      string
	UnitFilterPath     string
	UnitDetailPath     string
	UnitCustomCardPath string
	BattleMechFilter   string
}

func main() {

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Printf("Welcome to the mlu-scraper, go get yourself some Alphastrike data!\n")

	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Encountered error reading configuration file: %v\n", err)
	}

	config := Config{}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		log.Fatalf("Encountered error unmarshalling JSON config file to config object: %v\n", err)
	}

	log.Printf("Successfully read configuration file: %+v\n", config)

	response, err := http.Get(fmt.Sprintf("%v%v?%v", config.ScrapeBaseUrl, config.UnitFilterPath, config.BattleMechFilter))
	if err != nil {
		log.Fatalf("Encountered error requesting list of battlemechs from MLU: %v\n", err)
	}

	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("Encountered error reading filter request response: %v\n", err)
	}

	log.Printf("Successfully ran filter request against MLU. Number of bytes read: %v", len(responseBytes))
}
