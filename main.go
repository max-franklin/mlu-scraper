package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
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

	unitsToSearchFor := []*Unit{}

	for _, submatchSlice := range reMatches {
		unit := &Unit{
			Id:          string(submatchSlice[1]),
			Designation: string(submatchSlice[2]),
		}
		unitsToSearchFor = append(unitsToSearchFor, unit)
	}

	for _, unit := range unitsToSearchFor {
		unit.loadCustomCardDetails()
		unit.loadUnitOverviewDetails()
		log.Printf("Loaded details for unit: %+v", *unit)
	}

	jsonText, err := json.Marshal(unitsToSearchFor)
	if err != nil {
		log.Fatalf("Failed to marshal units to JSON text for output: %v\n", err)
	}

	fmt.Printf("\n%s\n", jsonText)

}

func (u *Unit) loadCustomCardDetails() {
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

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Encountered panic while parsing unit ( %v ) field data from html: %v\nStacktrace:\n%s", u.Designation, r, debug.Stack())
		}
	}()

	fieldAssignmentChannel := make(chan int, runtime.GOMAXPROCS(0))

	go func() {
		u.AlphaStrikeCardDetails.Name = getFieldDataForResponseMatcher(customCardResponse, nameRe)
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.Model = getFieldDataForResponseMatcher(customCardResponse, modelRe)
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.PV, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, pvRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.TP = getFieldDataForResponseMatcher(customCardResponse, typeRe)
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.SZ, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, sizeRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.MV, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, moveRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.ShortDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, shortRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.IsShortMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, shortMinRe), "checked")
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.MediumDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, mediumRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.IsMediumMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, mediumMinRe), "checked")
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.LongDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, longRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.IsLongMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, longMinRe), "checked")
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.ExtremeDamage, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, extremeRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.IsExtremeMinDamage = strings.Contains(getFieldDataForResponseMatcher(customCardResponse, extremeMinRe), "checked")
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.OV, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, ovRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.Armor, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, armorRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.Struc, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, strucRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.Threshold, _ = strconv.Atoi(getFieldDataForResponseMatcher(customCardResponse, thresholdRe))
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.Specials = getFieldDataForResponseMatcher(customCardResponse, specialsRe)
		fieldAssignmentChannel <- 1
	}()
	go func() {
		u.AlphaStrikeCardDetails.ImageUrl = getFieldDataForResponseMatcher(customCardResponse, imageRe)
		fieldAssignmentChannel <- 1
	}()

	// wait for all the assignments to finish
	for i := 0; i < 20; i++ {
		<-fieldAssignmentChannel
	}
}

func getFieldDataForResponseMatcher(response []byte, matcher *regexp.Regexp) string {
	subMatches := matcher.FindAllSubmatch(response, -1)
	if subMatches == nil {
		log.Printf("Failed to find any submatches using regex: %+v\n", *matcher)
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
	response, err := http.Get(fmt.Sprintf("%v%v/%v/%v", config.ScrapeBaseUrl, config.UnitDetailPath, u.Id, u.Designation))
	if err != nil {
		log.Fatalf("Encountered error making request to MLU: %v\n", err)
	}

	defer response.Body.Close()

	detailResponse, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("Encountered error reading request response: %v\n", err)
	}

	fieldAssignmentChannel := make(chan int, runtime.GOMAXPROCS(0))

	go func() {
		u.UnitOverview.BattleValue, _ = strconv.Atoi(strings.Join(strings.Split(getFieldDataForResponseMatcher(detailResponse, battleValueRe), ","), ""))
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.Cost, _ = strconv.Atoi(strings.Join(strings.Split(getFieldDataForResponseMatcher(detailResponse, costRe), ","), ""))
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.DateIntroduced, _ = strconv.Atoi(getFieldDataForResponseMatcher(detailResponse, dateIntroducedRe))
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.Era = getFieldDataForResponseMatcher(detailResponse, eraRe)
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.Notes = getFieldDataForResponseMatcher(detailResponse, notesRe)
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.RulesLevel = getFieldDataForResponseMatcher(detailResponse, rulesLevelRe)
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.Technology = getFieldDataForResponseMatcher(detailResponse, technologyRe)
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.Tonnage, _ = strconv.Atoi(getFieldDataForResponseMatcher(detailResponse, tonnageRe))
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.UnitRole = getFieldDataForResponseMatcher(detailResponse, unitRoleRe)
		fieldAssignmentChannel <- 1
	}()

	go func() {
		u.UnitOverview.UnitType = getFieldDataForResponseMatcher(detailResponse, unitTypeRe)
		fieldAssignmentChannel <- 1
	}()

	// wait for all the assignments to finish
	for i := 0; i < 10; i++ {
		<-fieldAssignmentChannel
	}
}
