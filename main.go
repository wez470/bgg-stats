package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ssilva/bggo"
)

var months = []string{"Jan", "Feb", "March", "April", "May", "June", "July", "Aug", "Sept", "Oct", "Nov", "Dec"}
var weekdays = []string{"Sun", "Mon", "Tues", "Wed", "Thurs", "Fri", "Sat"}

func main() {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.LoadHTMLFiles("./templates/bgg-stats.html", "./templates/not-found.html")

	router.GET("/bgg/user/:name/", func(c *gin.Context) {
		user, ok := c.Params.Get("name")
		if !ok {
			c.HTML(http.StatusNotFound, "not-found.html", gin.H{})
			return
		}
		year := c.Query("year")
		if year == "" {
			year = fmt.Sprintf("%d", time.Now().Year())
		}

		statsData, err := getStats(user, year, c.Request.URL)
		if err != nil {
			fmt.Println(err)
			c.HTML(http.StatusNotFound, "not-found.html", gin.H{})
			return
		}

		c.HTML(http.StatusOK, "bgg-stats.html", statsData)
	})

	router.Run(":8080")
}

type MonthGamePlays struct {
	Name  string
	Plays int
	Month int
}

type NamedGamePlays struct {
	Name          string
	Plays         int
	WinPercentage float64
}

type NamedGamePlayTime struct {
	Name          string
	PlayTimeHours float64
}

type GamePlays struct {
	Plays      int
	PlayerWins int
}

type GameInfo struct {
	Name            string
	MinPlayTimeMins int
	MaxPlayTimeMins int
	AvgPlayTimeMins int
	Complexity      float32
}

type PlayerPlays struct {
	PlayerName string
	Plays      int
}

func getStats(user string, year string, reqURL *url.URL) (*gin.H, error) {
	resp, err := retrievePlays(user, year)
	if err != nil {
		return nil, err
	}

	totalPlaysByMonth := make([]int, 12)
	allGamePlaysByMonth := make([]map[string]int, 12)
	topGamePlaysByMonth := make([]MonthGamePlays, 12)
	statsByGame := make(map[string]*GamePlays)
	gameIDs := []string{}
	gameInfoByName := make(map[string]GameInfo)
	playsByWeekday := make([]int, 7)
	playsByPlayer := make(map[string]int)
	playsByLocation := make(map[string]int)
	avgPlayTimeByGameMins := make(map[string]int)
	playsByComplexity := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}
	complexityByPlayTimeMins := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}
	var totalPlays int
	var totalWins int
	var totalPlayTimeMins float64
	for _, play := range resp.Plays {
		t, err := time.Parse("2006-01-02", play.Date)
		if err != nil {
			fmt.Println("failed to parse date: ", play.Date)
			continue
		}
		if len(play.Items) == 0 || len(play.Items) > 1 {
			return nil, fmt.Errorf("0 or greater than 1 item in play")
		}
		game := play.Items[0]

		if _, ok := statsByGame[game.Name]; !ok {
			// new game
			statsByGame[game.Name] = &GamePlays{}
			gameIDs = append(gameIDs, game.ObjectID)
		}
		gameStats := statsByGame[game.Name]
		gameStats.Plays += play.Quantity
		totalPlaysByMonth[t.Month()-1] += play.Quantity
		if allGamePlaysByMonth[t.Month()-1] == nil {
			allGamePlaysByMonth[t.Month()-1] = map[string]int{}
		}
		allGamePlaysByMonth[t.Month()-1][game.Name] += 1
		playsByWeekday[t.Weekday()] += play.Quantity
		for _, p := range play.Players {
			if p.Username == user {
				if p.Win {
					totalWins += play.Quantity
					gameStats.PlayerWins += play.Quantity
				}
			} else {
				playsByPlayer[p.Name] += play.Quantity
			}
		}
		if play.Location != "" {
			playsByLocation[play.Location] += 1
		}
		totalPlays += play.Quantity
	}

	// get info for each game
	things := retrieveGames(gameIDs)
	for _, thing := range things.Items {
		gameName := ""
		for _, n := range thing.Names {
			if n.Type == "primary" {
				gameName = n.Value
				break
			}
		}
		gameInfoByName[gameName] = GameInfo{
			Name:            gameName,
			MinPlayTimeMins: thing.MinPlayTime.Value,
			MaxPlayTimeMins: thing.MaxPlayTime.Value,
			AvgPlayTimeMins: (thing.MinPlayTime.Value + thing.MaxPlayTime.Value) / 2,
			Complexity:      thing.Ratings.AverageWeight.Value,
		}
	}

	// All games by plays
	gamePlaysList := make([]NamedGamePlays, 0, len(statsByGame))
	for game, stats := range statsByGame {
		winPercentage := toFixed(float64(stats.PlayerWins)/float64(stats.Plays)*100, 1)
		gamePlaysList = append(gamePlaysList, NamedGamePlays{game, stats.Plays, winPercentage})
		avgPlayTimeByGameMins[game] = gameInfoByName[game].AvgPlayTimeMins * stats.Plays
		gameComplexity := int(math.Max(math.Min(math.Ceil(float64(gameInfoByName[game].Complexity)), 5), 1))
		playsByComplexity[gameComplexity] += stats.Plays
		complexityByPlayTimeMins[gameComplexity] += avgPlayTimeByGameMins[game]
		totalPlayTimeMins += float64(avgPlayTimeByGameMins[game])

	}
	sort.Slice(gamePlaysList, func(i, j int) bool {
		return gamePlaysList[i].Plays > gamePlaysList[j].Plays
	})

	// Games play time list
	gamePlayTimeList := make([]NamedGamePlayTime, 0, len(avgPlayTimeByGameMins))
	for game, playTime := range avgPlayTimeByGameMins {
		gamePlayTimeList = append(gamePlayTimeList, NamedGamePlayTime{game, toFixed(float64(playTime)/60.0, 1)})
	}
	sort.Slice(gamePlayTimeList, func(i, j int) bool {
		return gamePlayTimeList[i].PlayTimeHours > gamePlayTimeList[j].PlayTimeHours
	})

	// Top ten
	var topTenGamesByPlays []NamedGamePlays
	var topTenGamesByPlayTime []NamedGamePlayTime
	for i := 0; i < 10; i++ {
		if i >= len(gamePlaysList) {
			break
		}
		topTenGamesByPlays = append(topTenGamesByPlays, gamePlaysList[i])
		topTenGamesByPlayTime = append(topTenGamesByPlayTime, gamePlayTimeList[i])
	}

	// Percentage plays by game
	var gameNames []string
	var gamePercentages []float64
	otherGamesPercent := 0.0
	// limit players to 20 graph keys (19 + other)
	for i, gamePlay := range gamePlaysList {
		percent := float64(gamePlay.Plays) / float64(totalPlays) * 100.0
		if i >= 19 {
			otherGamesPercent += percent
			continue
		}
		gameNames = append(gameNames, gamePlay.Name)
		gamePercentages = append(gamePercentages, toFixed(float64(gamePlay.Plays)/float64(totalPlays)*100, 1))
	}
	if otherGamesPercent > 0 {
		gameNames = append(gameNames, "Other")
		gamePercentages = append(gamePercentages, toFixed(otherGamesPercent, 1))
	}

	// Plays per player
	playsPerPlayerList := make([]PlayerPlays, len(playsByPlayer))
	for player, plays := range playsByPlayer {
		playsPerPlayerList = append(playsPerPlayerList, PlayerPlays{PlayerName: player, Plays: plays})
	}
	sort.Slice(playsPerPlayerList, func(i, j int) bool {
		return playsPerPlayerList[i].Plays > playsPerPlayerList[j].Plays
	})
	// limit players to 20 graph keys (19 + other)
	var playerNames []string
	var playerPlays []int
	var otherPlayersPlays int
	for i, playerPlay := range playsPerPlayerList {
		if i >= 19 {
			otherPlayersPlays += playerPlay.Plays
			continue
		}
		playerNames = append(playerNames, playerPlay.PlayerName)
		playerPlays = append(playerPlays, playerPlay.Plays)
	}
	if otherPlayersPlays > 0 {
		playerNames = append(playerNames, "Other")
		playerPlays = append(playerPlays, otherPlayersPlays)
	}

	// Available years
	var availYears []int
	currYear := time.Now().Year()
	for i := 0; i <= 10; i++ {
		availYears = append(availYears, currYear-i)
	}
	// Curr selected year
	var queryVals url.Values
	queryVals, err = url.ParseQuery(reqURL.RawQuery)
	if err != nil {
		return nil, err
	}
	selectedYear := strconv.Itoa(currYear)
	if queryVals.Has("year") {
		selectedYear = queryVals.Get("year")
	}
	for i := 0; i < len(months); i++ {
		allGamesForMonth := allGamePlaysByMonth[i]
		gameNamesForMonth := make([]string, 0, len(allGamesForMonth))
		for n, _ := range allGamesForMonth {
			gameNamesForMonth = append(gameNamesForMonth, n)
		}
		sort.SliceStable(gameNamesForMonth, func(i, j int) bool {
			return allGamesForMonth[gameNamesForMonth[i]] > allGamesForMonth[gameNamesForMonth[j]]
		})
		if len(gameNamesForMonth) > 0 {
			topGamePlaysByMonth[i] = MonthGamePlays{
				Name:  gameNamesForMonth[0],
				Plays: allGamesForMonth[gameNamesForMonth[0]],
				Month: i,
			}
			// subtract the months top game from total plays since it'll be shown as a seperate bar
			totalPlaysByMonth[i] -= allGamesForMonth[gameNamesForMonth[0]]
		}
	}
	// Plays by location
	var locationNames []string
	var locationPlays []int
	for location, plays := range playsByLocation {
		locationNames = append(locationNames, location)
		locationPlays = append(locationPlays, plays)
	}

	// Complexity names / values
	var complexityNames []string
	var complexityPlays []int
	var complexityPlayTimeHours []float64
	for i := 1; i <= 5; i++ {
		complexity := i
		plays := playsByComplexity[complexity]
		complexityNames = append(complexityNames, fmt.Sprintf("%d-%d", complexity-1, complexity))
		complexityPlays = append(complexityPlays, plays)
		complexityPlayTimeHours = append(complexityPlayTimeHours, toFixed(float64(complexityByPlayTimeMins[complexity])/60.0, 1))
	}

	return &gin.H{
		"weekdays":              weekdays,
		"playsByWeekday":        playsByWeekday,
		"months":                months,
		"uniquePlays":           len(statsByGame),
		"playsByMonth":          totalPlaysByMonth,
		"avgPlaytimeByGame":     avgPlayTimeByGameMins,
		"complexityNames":       complexityNames,
		"complexityPlays":       complexityPlays,
		"complexityPlayTime":    complexityPlayTimeHours,
		"topGameByMonth":        topGamePlaysByMonth,
		"topTenGamesByPlays":    topTenGamesByPlays,
		"topTenGamesByPlayTime": topTenGamesByPlayTime,
		"gameNames":             gameNames,
		"gamePercentages":       gamePercentages,
		"playerNames":           playerNames,
		"playerPlays":           playerPlays,
		"locationNames":         locationNames,
		"locationPlays":         locationPlays,
		"winPercentage":         toFixed(float64(totalWins)/float64(totalPlays)*100, 1),
		"totalPlays":            totalPlays,
		"totalHours":            toFixed(totalPlayTimeMins/60.0, 1),
		"years":                 availYears,
		"path":                  reqURL.Path,
		"selectedYear":          selectedYear,
	}, nil
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}

func parseMonth(date string) (int, error) {
	dateParts := strings.Split(date, "-")
	if len(dateParts) != 3 {
		return 0, fmt.Errorf("failed to parse date")
	}
	monthInt, err := strconv.Atoi(dateParts[1])
	if err != nil {
		return 0, err
	}
	return monthInt, nil
}

// largely copied from github.com/ssilva/bggo/cmd to get things started

const (
	bggurlplays      string = "https://www.boardgamegeek.com/xmlapi2/plays?username=%s&mindate=%s&maxdate=%s&page=%d"
	bggurlsearch     string = "https://www.boardgamegeek.com/xmlapi2/search?type=boardgame&query="
	bggurlthing      string = "https://www.boardgamegeek.com/xmlapi2/thing?stats=1&id="
	bggurlhot        string = "https://www.boardgamegeek.com/xmlapi2/hot?type=boardgame"
	bggurlcollection string = "https://www.boardgamegeek.com/xmlapi2/collection?own=1&stats=1&username="
)

func retrievePlays(username, year string) (*bggo.PlaysResponse, error) {
	totalResp := &bggo.PlaysResponse{}
	page := 1

	for {
		playsURL := fmt.Sprintf(bggurlplays, url.QueryEscape(username), url.QueryEscape(fmt.Sprintf("%s-01-01", year)), url.QueryEscape(fmt.Sprintf("%s-12-31", year)), page)
		xmldata := httpGetAndReadAll(playsURL)

		pageResp := &bggo.PlaysResponse{}
		err := xml.Unmarshal(xmldata, pageResp)
		if err != nil {
			return nil, err
		}

		totalResp.Plays = append(totalResp.Plays, pageResp.Plays...)

		if len(pageResp.Plays) < 100 {
			break
		}
		page++
	}

	return totalResp, nil
}

func printPlays(resp *bggo.PlaysResponse) {
	fmt.Printf("Last %d plays for %s\n", len(resp.Plays), resp.Username)
	for _, play := range resp.Plays {
		fmt.Printf("\t%s: ", play.Date)
		for i, item := range play.Items {
			fmt.Printf("%s", item.Name)
			if i < (len(play.Items) - 1) {
				fmt.Print(", ")
			}
		}
		if len(play.Players) > 0 {
			fmt.Printf(" [")
			for i, player := range play.Players {
				if player.Name != "" {
					fmt.Printf("%s", player.Name)
				} else {
					fmt.Printf("%s", player.Username)
				}
				if player.Score != "" {
					fmt.Printf(" - %s", player.Score)
				}
				if i < (len(play.Players) - 1) {
					fmt.Print(", ")
				}
			}
			fmt.Printf("]")
		}

		fmt.Println()
	}
}

func retrieveGames(gameIDs []string) (resp *bggo.ThingResponse) {
	gameIDBatches := slices.Chunk(gameIDs, 20)
	allResp := &bggo.ThingResponse{}
	for batch := range gameIDBatches {
		allResp.Items = append(allResp.Items, retrieveGames20(strings.Join(batch, ",")).Items...)
	}
	return allResp
}

// `gameIDs` is a comma-separated list of game IDs. Max 20 items
func retrieveGames20(gameIDs string) (resp *bggo.ThingResponse) {
	xmldata := httpGetAndReadAll(bggurlthing + gameIDs)
	resp = &bggo.ThingResponse{}
	unmarshalOrDie(xmldata, resp)
	return
}

func printGames(resp *bggo.ThingResponse) {
	for _, item := range resp.Items {
		fmt.Printf("[%.1f] (%5d votes, rank %3s) %s\n",
			item.Ratings.Average.Value,
			item.Ratings.UsersRated.Value,
			item.Ratings.BoardGameRank(),
			item.PrimaryName(),
		)
	}
}

func searchGame(gameName string, exactSearch bool) (resp *bggo.SearchResponse) {
	url := bggurlsearch + url.QueryEscape(gameName)
	if exactSearch {
		url += "&exact=1"
	}

	xmldata := httpGetAndReadAll(url)
	resp = &bggo.SearchResponse{}
	unmarshalOrDie(xmldata, resp)

	return
}

func retrieveAndPrintGameRating(gameName string, exactSearch bool) {
	results := searchGame(gameName, exactSearch)

	if results.Total == 0 {
		fmt.Println("Search returned no items")
		return
	}

	for _, item := range results.Items {
		game := retrieveGames20(item.ID)
		printGames(game)
	}
}

func retrieveAndPrintHotGames() {
	xmldata := httpGetAndReadAll(bggurlhot)
	resp := &bggo.HotResponse{}
	unmarshalOrDie(xmldata, resp)

	for _, item := range resp.Items {
		fmt.Printf("[%2d] %s (%s)\n", item.Rank, item.Name.Value, item.YearPublished.Value)
	}

	return
}

func retrieveCollection(username string) (coll *bggo.CollectionResponse) {
	xmldata := httpGetAndReadAll(bggurlcollection + username)
	coll = &bggo.CollectionResponse{}
	unmarshalOrDie(xmldata, coll)
	return
}

// printCollectionStats prints the top `limit` items of a map holding collection statistics of a
// particular type (e.g. number of games owned, grouped by boardgame designers)
func printCollectionStats(limit int, collectionStats *map[string]int, collectionStatsLabel string) {
	keyValues := sortMapByValue(collectionStats)

	fmt.Printf("Top %d %s\n", limit, collectionStatsLabel)
	for _, keyvalue := range keyValues[:limit] {
		fmt.Printf("\t%s [%d]\n", keyvalue.key, keyvalue.value)
	}
}

type keyvalue struct {
	key   string
	value int
}

func sortMapByValue(items *map[string]int) (keyValues []keyvalue) {
	keyValues = make([]keyvalue, 0, len(*items))

	for k, v := range *items {
		keyValues = append(keyValues, keyvalue{k, v})
	}

	sort.Slice(keyValues, func(i, j int) bool {
		return keyValues[i].value > keyValues[j].value
	})

	return
}

func httpGetAndReadAll(url string) (xmldata []byte) {
	var (
		retries = 3
		resp    *http.Response
		err     error
	)

	for retries > 0 {
		resp, err = http.Get(url)
		if err != nil || resp.StatusCode == 202 {
			retries--
			time.Sleep(400 * time.Millisecond)
		} else {
			break
		}
	}

	if resp != nil {
		defer resp.Body.Close()

		xmldata, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("ERROR %s", err)
		}
	} else {
		log.Fatalf("ERROR %s", err)
	}

	return
}

func unmarshalOrDie(xmldata []byte, object interface{}) {
	err := xml.Unmarshal(xmldata, object)
	if err != nil {
		log.Fatalf("ERROR %s", err)
	}
	return
}
