package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ScrapeBaseUrl      string
	UnitFilterPath     string
	UnitDetailPath     string
	UnitCustomCardPath string
	BattleMechFilter   string
}

type Unit struct {
	Id                     string
	Designation            string
	AlphaStrikeCardDetails AlphaStrikeCard
	UnitOverview           UnitDetails
}

type AlphaStrikeCard struct {
	Name               string
	Model              string
	PV                 int
	TP                 string
	SZ                 int
	MV                 string
	Role               string
	Skill              int
	ShortDamage        int
	IsShortMinDamage   bool
	MediumDamage       int
	IsMediumMinDamage  bool
	LongDamage         int
	IsLongMinDamage    bool
	ExtremeDamage      int
	IsExtremeMinDamage bool
	OV                 int
	Armor              int
	Struc              int
	Threshold          int
	Specials           string
	ImageUrl           string
}

type UnitDetails struct {
	Tonnage        int
	BattleValue    int
	Cost           int
	RulesLevel     string
	Technology     string
	UnitType       string
	UnitRole       string
	DateIntroduced int
	Era            string
	Notes          string
}

var nameRe = regexp.MustCompile(`id="Data_Name".*value="(.*)"`)
var modelRe = regexp.MustCompile(`id="Data_Model".*value="(.*)"`)
var pvRe = regexp.MustCompile(`id="Data_PV".*value="(.*)"`)
var typeRe = regexp.MustCompile(`id="Data_Type".*value="(.*)"`)
var sizeRe = regexp.MustCompile(`id="Data_Size".*value="(.*)"`)
var moveRe = regexp.MustCompile(`id="Data_Move".*value="(.*)"`)
var shortRe = regexp.MustCompile(`id="Data_Short".*value="(.*)"`)
var shortMinRe = regexp.MustCompile(`(.*)id="Data_ShortMin"`)
var mediumRe = regexp.MustCompile(`id="Data_Medium".*value="(.*)"`)
var mediumMinRe = regexp.MustCompile(`(.*)id="Data_MediumMin"`)
var longRe = regexp.MustCompile(`id="Data_Long".*value="(.*)"`)
var longMinRe = regexp.MustCompile(`(.*)id="Data_LongMin"`)
var extremeRe = regexp.MustCompile(`id="Data_Extreme".*value="(.*)"`)
var extremeMinRe = regexp.MustCompile(`id="Data_ExtremeMin"(.*)`)
var ovRe = regexp.MustCompile(`id="Data_Overheat".*value="(.*)"`)
var armorRe = regexp.MustCompile(`id="Data_Armor".*value="(.*)"`)
var strucRe = regexp.MustCompile(`id="Data_Structure".*value="(.*)"`)
var thresholdRe = regexp.MustCompile(`id="Data_Threshold".*value="(.*)"`)
var specialsRe = regexp.MustCompile(`id="Data_Specials".*\>.*\n(.*)\n.*\<`)
var imageRe = regexp.MustCompile(`id="Data_Image".*\>.*\n(.*)\n.*\<`)
var tonnageRe = regexp.MustCompile(`.*<dt>Tonnage</dt>.*\n.*<dd>(\d+)</dd>`)
var battleValueRe = regexp.MustCompile(`.*<dt>Battle Value</dt>.*\n.*<dd>(\w+)</dd>`)
var costRe = regexp.MustCompile(`.*<dt>Cost</dt>.*\n.*<dd>(\w+)</dd>`)
var rulesLevelRe = regexp.MustCompile(`.*<dt>Rules Level</dt>.*\n.*<dd>(\w+)</dd>`)
var technologyRe = regexp.MustCompile(`.*<dt>Technology</dt>.*\n.*<dd>([a-zA-Z0-9 ,]+)</dd>`)
var unitTypeRe = regexp.MustCompile(`.*<dt>Unit Type</dt>.*\n.*<dd>([a-zA-Z0-9 ,]+)</dd>`)
var unitRoleRe = regexp.MustCompile(`.*<dt>Unit Role</dt>.*\n.*<dd>([a-zA-Z0-9 ,]+)</dd>`)
var dateIntroducedRe = regexp.MustCompile(`.*<dt>Date Introduced</dt>.*\n.*<dd>(\d+)</dd>`)
var eraRe = regexp.MustCompile(`.*<dt>Era</dt>.*\n.*<dd>([a-zA-Z0-9 ,\(\)-]+)</dd>`)
var notesRe = regexp.MustCompile(`.*<dt>Notes</dt>.*\n.*<dd>([a-zA-Z0-9 ,\(\)]+)</dd>`)

var config Config

func main() {

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Printf("Welcome to the mlu-scraper, go get yourself some Alphastrike data!\n")

	config = loadConfig()

	alreadyFoundUnitsMap := loadAlreadyScrapedUnits()

	unitsToSearchFor := loadUnitsToSearchFor(alreadyFoundUnitsMap)

	start := time.Now()

	// write progress to disk incase we need to start back up from an interrupt
	writeUnitDataChan := make(chan *Unit)
	writeUnitDataFinishedChan := make(chan int)
	go startDataDumpingProcessor(writeUnitDataChan, writeUnitDataFinishedChan)

	// Launch n many go routines for scraping unit details
	unitProcessingChan := make(chan *Unit, len(unitsToSearchFor))
	processedUnitsChan := make(chan int, len(unitsToSearchFor))
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go startUnitScraper(unitProcessingChan, writeUnitDataChan, processedUnitsChan)
	}

	numberOfUnitsToProcess := len(unitsToSearchFor)
	for i := 0; i < numberOfUnitsToProcess; i++ {
		unitToProcess := unitsToSearchFor[i]
		unitProcessingChan <- unitToProcess
	}

	log.Printf("Loaded up all units for processing\n")

	// Wait until we've received all the results
	tenPercent := int(math.Ceil((float64(numberOfUnitsToProcess) * 10.0) / 100.0))
	for i := 0; i < numberOfUnitsToProcess; i += <-processedUnitsChan {
		if i%tenPercent == 0 {
			percentComplete := 10 * (i / tenPercent)
			log.Printf("%v%% complete with processing units. Batch size: %v, Current unit number in batch: %v\n", percentComplete, numberOfUnitsToProcess, i)
		}
	}

	// Close up the scraping channel, not strictly necessary since
	// we're waiting for all the units to be processed first.
	close(unitProcessingChan)

	// Close up the send channel and wait for it to tell us its done writing
	close(writeUnitDataChan)
	<-writeUnitDataFinishedChan

	log.Printf("Total time spent processing unit details: %v", time.Since(start))
}

func loadConfig() Config {
	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Encountered error reading configuration file: %v\n", err)
	}

	c := Config{}
	if err := json.Unmarshal(configBytes, &c); err != nil {
		log.Fatalf("Encountered error unmarshalling JSON config file to config object: %v\n", err)
	}

	log.Printf("Successfully read configuration file: %+v\n", c)

	return c
}

func loadAlreadyScrapedUnits() map[string]int {
	alreadyFoundUnitsMap := map[string]int{}
	remaining_units_bytes, err := os.ReadFile("unit_scrape_results.json")
	if err == nil {
		log.Printf("Loading pre-existing dump file to attempt to resume from...\n")

		scanner := bufio.NewScanner(bytes.NewReader(remaining_units_bytes))
		for scanner.Scan() {
			unit := &Unit{}
			err = json.Unmarshal(bytes.TrimSpace(scanner.Bytes()), unit)
			if err != nil {
				log.Fatalf("Failed to load pre-existing file unit data for resuming search: %v\n", err)
			}
			alreadyFoundUnitsMap[unit.Id] = 1
		}

		log.Printf("Successfully loaded pre-existing file for units already found: %v\n", alreadyFoundUnitsMap)
	}

	return alreadyFoundUnitsMap
}

func startDataDumpingProcessor(writeUnitDataChan chan *Unit, writeUnitDataFinishedChan chan int) {
	f, err := os.OpenFile("unit_scrape_results.json", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("Failed to open progress file for dumping processed unit data\n")
	}
	defer f.Close()

	for unitToWrite := range writeUnitDataChan {
		log.Printf("Received unit to write to json file: \n\n%+v\n\n", *unitToWrite)

		jsonText, err := json.Marshal(unitToWrite)
		if err != nil {
			log.Fatalf("Failed to marshal units to JSON text for output: %v\n", err)
		}

		// Add a newline to the json text
		nl := []byte("\n")
		jsonText = append(jsonText, nl...)

		n, err := f.Write(jsonText)
		if err != nil {
			log.Fatalf("Failed to write %v bytes to unit scrape result file: %v\nBytes: %s\n", n, err, jsonText)
		}

		log.Printf("Successfully wrote %v bytes to unit scrape result file:\n%s\n", n, jsonText)
	}

	log.Printf("Received signal to close channel for writing unit data, sending finished signal to main process\n")

	writeUnitDataFinishedChan <- 1
}

func startUnitScraper(unitsToProcess chan *Unit, writeDataToFile chan *Unit, notifyMainCallerOfProcessedUnit chan int) {
	// We do this so that go routines don't inadvertantly share the same unit
	for unit := range unitsToProcess {
		gostart := time.Now()
		unit.loadCustomCardDetails()
		unit.loadUnitOverviewDetails()

		if unit.AlphaStrikeCardDetails.Role == "" {
			unit.AlphaStrikeCardDetails.Role = unit.UnitOverview.UnitRole
		}

		log.Printf("Finished loading details for unit: %v Time spent: %v", unit.Designation, time.Since(gostart))

		writeDataToFile <- unit
		log.Printf("Wrote unit to write unit data channel\n")

		notifyMainCallerOfProcessedUnit <- 1
		log.Printf("Wrote signal to processed units channel\n")
	}
}

func loadUnitsToSearchFor(alreadyFoundUnitsMap map[string]int) []*Unit {
	unitsToSearchFor := []*Unit{}

	// Make unit filter request
	response, err := http.Get(fmt.Sprintf("%v%v?%v", config.ScrapeBaseUrl, config.UnitFilterPath, config.BattleMechFilter))
	if err != nil {
		log.Fatalf("Encountered error making request to MLU: %v\n", err)
	}

	defer response.Body.Close()

	filterResponse, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("Encountered error reading request response: %v\n", err)
	}

	re, err := regexp.Compile(`"/Unit/Details/(\d+)/(.*)"`)
	if err != nil {
		log.Fatalf("Failed to compile regexp for matching against scraped page: %v\n", err)
	}

	reMatches := re.FindAllSubmatch(filterResponse, -1)

	for _, submatchSlice := range reMatches {
		if _, ok := alreadyFoundUnitsMap[string(submatchSlice[1])]; !ok {
			unit := &Unit{
				Id:          string(submatchSlice[1]),
				Designation: string(submatchSlice[2]),
			}
			unitsToSearchFor = append(unitsToSearchFor, unit)
		} else {
			log.Printf("Detected already scraper unit: %v - %v, skipping scrape for it\n", string(submatchSlice[1]), string(submatchSlice[2]))
		}
	}

	return unitsToSearchFor
}

func (u *Unit) loadCustomCardDetails() {

	s := time.Now()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("detected redirect when attempting to access client custom card path")
		},
	}

	response, err := client.Get(fmt.Sprintf("%v%v/%v", config.ScrapeBaseUrl, config.UnitCustomCardPath, u.Id))

	if err != nil {
		if strings.Contains(err.Error(), "detected redirect when attempting to access client custom card path") {
			return
		}
		log.Fatalf("Encountered error making request to MLU: %v\n", err)
	}

	defer response.Body.Close()

	customCardResponse, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("Encountered error reading request response: %v\n", err)
	}

	log.Printf("Time spent making custom card http request: %v\n", time.Since(s))

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Encountered panic while parsing unit ( %v ) field data from html: %v\nStacktrace:\n%s", u.Designation, r, debug.Stack())
		}
	}()

	u.AlphaStrikeCardDetails.Name = getFieldDataForResponseMatcher(customCardResponse, nameRe)
	u.AlphaStrikeCardDetails.Model = getFieldDataForResponseMatcher(customCardResponse, modelRe)
	u.AlphaStrikeCardDetails.PV, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, pvRe))
	u.AlphaStrikeCardDetails.TP = getFieldDataForResponseMatcher(customCardResponse, typeRe)
	u.AlphaStrikeCardDetails.SZ, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, sizeRe))
	u.AlphaStrikeCardDetails.MV = getFieldDataForResponseMatcher(customCardResponse, moveRe)
	u.AlphaStrikeCardDetails.ShortDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, shortRe))
	u.AlphaStrikeCardDetails.IsShortMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, shortMinRe), "checked")
	u.AlphaStrikeCardDetails.MediumDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, mediumRe))
	u.AlphaStrikeCardDetails.IsMediumMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, mediumMinRe), "checked")
	u.AlphaStrikeCardDetails.LongDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, longRe))
	u.AlphaStrikeCardDetails.IsLongMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, longMinRe), "checked")
	u.AlphaStrikeCardDetails.ExtremeDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, extremeRe))
	u.AlphaStrikeCardDetails.IsExtremeMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, extremeMinRe), "checked")
	u.AlphaStrikeCardDetails.OV, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, ovRe))
	u.AlphaStrikeCardDetails.Armor, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, armorRe))
	u.AlphaStrikeCardDetails.Struc, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, strucRe))
	u.AlphaStrikeCardDetails.Threshold, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, thresholdRe))
	u.AlphaStrikeCardDetails.Specials = getFieldDataForResponseMatcher(customCardResponse, specialsRe)
	u.AlphaStrikeCardDetails.ImageUrl = getFieldDataForResponseMatcher(customCardResponse, imageRe)
	u.AlphaStrikeCardDetails.Skill = 4
}

func getFieldDataForResponseMatcher(response []byte, matcher *regexp.Regexp) string {
	subMatches := matcher.FindAllSubmatch(response, -1)
	if subMatches == nil {
		// log.Printf("Failed to find any submatches using regex: %+v\n", *matcher)
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Encountered panic while parsing unit field data from html: %v\nStacktrace:\n%s", r, debug.Stack())
		}
	}()
	return string(subMatches[0][1])
}

func (u *Unit) loadUnitOverviewDetails() {
	s := time.Now()

	response, err := http.Get(fmt.Sprintf("%v%v/%v/%v", config.ScrapeBaseUrl, config.UnitDetailPath, u.Id, u.Designation))
	if err != nil {
		log.Fatalf("Encountered error making request to MLU: %v\n", err)
	}

	defer response.Body.Close()

	detailResponse, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("Encountered error reading request response: %v\n", err)
	}

	log.Printf("Time spent making unit overview http request: %v\n", time.Since(s))

	u.UnitOverview.BattleValue, _ = strconv.Atoi(strings.Join(strings.Split(getFieldDataForResponseMatcher(detailResponse, battleValueRe), ","), ""))
	u.UnitOverview.Cost, _ = strconv.Atoi(strings.Join(strings.Split(getFieldDataForResponseMatcher(detailResponse, costRe), ","), ""))
	u.UnitOverview.DateIntroduced, _ = strconv.Atoi(getFieldDataForResponseMatcher(detailResponse, dateIntroducedRe))
	u.UnitOverview.Era = getFieldDataForResponseMatcher(detailResponse, eraRe)
	u.UnitOverview.Notes = getFieldDataForResponseMatcher(detailResponse, notesRe)
	u.UnitOverview.RulesLevel = getFieldDataForResponseMatcher(detailResponse, rulesLevelRe)
	u.UnitOverview.Technology = getFieldDataForResponseMatcher(detailResponse, technologyRe)
	u.UnitOverview.Tonnage, _ = strconv.Atoi(getFieldDataForResponseMatcher(detailResponse, tonnageRe))
	u.UnitOverview.UnitRole = getFieldDataForResponseMatcher(detailResponse, unitRoleRe)
	u.UnitOverview.UnitType = getFieldDataForResponseMatcher(detailResponse, unitTypeRe)
}
