package main

import (
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
	"slices"
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
	MV                 int
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
var shortMinRe = regexp.MustCompile(`id="Data_ShortMin"(.*)`)
var mediumRe = regexp.MustCompile(`id="Data_Medium".*value="(.*)"`)
var mediumMinRe = regexp.MustCompile(`id="Data_MediumMin"(.*)`)
var longRe = regexp.MustCompile(`id="Data_Long".*value="(.*)"`)
var longMinRe = regexp.MustCompile(`id="Data_LongMin"(.*)`)
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

	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Encountered error reading configuration file: %v\n", err)
	}

	config = Config{}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		log.Fatalf("Encountered error unmarshalling JSON config file to config object: %v\n", err)
	}

	log.Printf("Successfully read configuration file: %+v\n", config)

	unitsToSearchFor := []*Unit{}

	remaining_units_bytes, err := os.ReadFile("remaining_units.json")
	if err != nil && os.IsNotExist(err) {
		log.Printf("Didn't detect any progress files to resume from, starting a fresh filter request\n")
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
			unit := &Unit{
				Id:          string(submatchSlice[1]),
				Designation: string(submatchSlice[2]),
			}
			unitsToSearchFor = append(unitsToSearchFor, unit)
		}
	} else {
		log.Printf("Loading progress file to resume from...\n")

		err = json.Unmarshal(remaining_units_bytes, &unitsToSearchFor)
		if err != nil {
			log.Fatalf("Failed to load progress file unit data for resuming search: %v\n", err)
		}

		log.Printf("Successfully loaded progress file for units to search for, remaining units: %v", len(unitsToSearchFor))
	}

	remainingUnits := unitsToSearchFor[:]

	start := time.Now()

	// write progress to disk incase we need to start back up from an interrupt
	writeUnitDataChannel := make(chan *Unit)

	f, err := os.OpenFile("unit_scrape_results.json", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("Failed to open progress file for dumping processed unit data\n")
	}

	go func() {
		for unit := range writeUnitDataChannel {
			jsonText, err := json.Marshal(unit)
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

			i := slices.Index(remainingUnits, unit)
			remainingUnits = append(remainingUnits[:i], remainingUnits[(i+1):]...)

			remainingUnitsJson, err := json.Marshal(remainingUnits)
			if err != nil {
				log.Fatalf("Failed to marshal remaining units to JSON text for output: %v\n", err)
			}

			err = os.WriteFile("remaining_units.json", remainingUnitsJson, 0644)
			if err != nil {
				log.Fatalf("Failed to write remaining units to process to file: %v\n", err)
			}
		}
		log.Printf("Received signal to close channel for writing unit data\n")
	}()

	defer f.Close()

	maxRequestFlightChan := make(chan int, runtime.GOMAXPROCS(0))
	numberOfUnitsToProcess := len(unitsToSearchFor)
	tenPercent := int(math.Ceil((float64(numberOfUnitsToProcess) * 10.0) / 100.0))
	processedUnitsChan := make(chan int)
	for i := 0; i < numberOfUnitsToProcess; i++ {
		unitToProcess := unitsToSearchFor[i]

		if i%tenPercent == 0 {
			percentComplete := 10 * (i / tenPercent)
			fmt.Printf("%v%% complete with processing units. Batch size: %v, Current unit number in batch: %v\n", percentComplete, numberOfUnitsToProcess, i)
		}

		maxRequestFlightChan <- 1
		go func() {
			// We do this so that go routines don't inadvertantly share the same unit
			unit := unitToProcess
			gostart := time.Now()
			unit.loadCustomCardDetails()
			unit.loadUnitOverviewDetails()

			log.Printf("Finished loading details for unit: %v Time spent: %v", unit.Designation, time.Since(gostart))

			writeUnitDataChannel <- unit

			<-maxRequestFlightChan
			processedUnitsChan <- 1
		}()
	}

	// Wait until we've received all the results
	for i := 0; i < numberOfUnitsToProcess; i++ {
		<-processedUnitsChan
	}

	close(writeUnitDataChannel)

	log.Printf("Total time spent processing unit details: %v", time.Since(start))

	// clean up the progress file if necessary
	os.Remove("remaining_units.json")
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
	u.AlphaStrikeCardDetails.MV, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, moveRe))
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
