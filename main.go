package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
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

		statsData, err := getStats(user, year)
		if err != nil {
			fmt.Println(err)
			c.HTML(http.StatusNotFound, "not-found.html", gin.H{})
			return
		}

		c.HTML(http.StatusOK, "bgg-stats.html", statsData)
	})

	router.Run(":8080")
}

type GamePlays struct {
	Name  string
	Plays int
}

func getStats(user string, year string) (*gin.H, error) {
	resp, err := retrievePlays(user, year)
	if err != nil {
		return nil, err
	}

	playsByMonth := make([]int, 12)
	playsByGame := make(map[string]int)
	playsByWeekday := make([]int, 7)
	var totalPlays int
	for _, play := range resp.Plays {
		t, err := time.Parse("2006-01-02", play.Date)
		if err != nil {
			fmt.Println("failed to parse date: ", play.Date)
			continue
		}
		if len(play.Items) < 0 || len(play.Items) > 1 {
			return nil, fmt.Errorf("more than 1 item in play")
		}
		game := play.Items[0]

		playsByMonth[t.Month()-1] += play.Quantity
		playsByGame[game.Name] += play.Quantity
		playsByWeekday[t.Weekday()] += play.Quantity
		totalPlays += play.Quantity
	}

	// All games by plays
	gamePlaysList := make([]GamePlays, 0, len(playsByGame))
	for g, p := range playsByGame {
		gamePlaysList = append(gamePlaysList, GamePlays{g, p})
	}
	sort.Slice(gamePlaysList, func(i, j int) bool {
		return gamePlaysList[i].Plays > gamePlaysList[j].Plays
	})

	// Top ten
	var topTenGamesByPlays []GamePlays
	for i := 0; i < 10; i++ {
		if i >= len(gamePlaysList) {
			break
		}
		topTenGamesByPlays = append(topTenGamesByPlays, gamePlaysList[i])
	}

	// Percentage
	var gameNames []string
	var gamePercentages []int
	otherPercent := 0.0
	for _, gamePlay := range gamePlaysList {
		percent := float64(gamePlay.Plays) / float64(totalPlays) * 100.0
		if percent < 1.0 {
			otherPercent += percent
			continue
		}
		gameNames = append(gameNames, gamePlay.Name)
		gamePercentages = append(gamePercentages, int(float64(gamePlay.Plays)/float64(totalPlays)*100.0))
	}
	if otherPercent > 0 {
		gameNames = append(gameNames, "Other")
		gamePercentages = append(gamePercentages, int(otherPercent))
	}

	return &gin.H{
		"weekdays":           weekdays,
		"playsByWeekday":     playsByWeekday,
		"months":             months,
		"playsByMonth":       playsByMonth,
		"topTenGamesByPlays": topTenGamesByPlays,
		"gameNames":          gameNames,
		"gamePercentages":    gamePercentages,
	}, nil
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

func printHelp() {
	fmt.Println("bggo: Get statistics from BoardGameGeek.com")
	fmt.Println()
	fmt.Println("To get the rating of a board game:")
	fmt.Println("  bggo GAMENAME")
	fmt.Println()
	fmt.Println("To get the rating of a board game, using exact search:")
	fmt.Println("  bggo -exact GAMENAME")
	fmt.Println()
	fmt.Println("To get a user's plays:")
	fmt.Println("  bggo -plays USERNAME")
	fmt.Println()
	fmt.Println("To get statistcs on a user's collection of owned games:")
	fmt.Println("  bggo -collection USERNAME")
	fmt.Println()
	fmt.Println("To get the list of most active games:")
	fmt.Println("  bggo -hot")
}

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

// `gameIDs` is a comma-separated list of game IDs
func retrieveGames(gameIDs string) (resp *bggo.ThingResponse) {
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
		game := retrieveGames(item.ID)
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

type customstats struct {
	// Stats based on CollectionResponse
	mostPlayedName  string
	mostPlayedCount int

	// Stats based on ThingResponmse
	designers  map[string]int
	mechanics  map[string]int
	categories map[string]int

	mostPopularName  string
	mostPopularCount int

	leastPopularName  string
	leastPopularCount int

	highestRatedName  string
	highestRatedAvg   float32
	highestRatedVotes int

	lowestRatedName  string
	lowestRatedAvg   float32
	lowestRatedVotes int
}

func makeCustomStats() *customstats {
	return &customstats{
		designers:         make(map[string]int),
		mechanics:         make(map[string]int),
		categories:        make(map[string]int),
		leastPopularCount: math.MaxUint32,
		lowestRatedAvg:    math.MaxFloat32,
	}
}

func collectStatsOnCollection(stats *customstats, coll *bggo.CollectionResponse) {
	for _, item := range coll.Items {
		if item.NumPlays >= stats.mostPlayedCount {
			stats.mostPlayedName = item.Name.Value
			stats.mostPlayedCount = item.NumPlays
		}
	}
}

func collectStatsOnGames(stats *customstats, games *bggo.ThingResponse) {
	for _, g := range games.Items {
		for _, link := range g.Links {
			switch link.Type {
			case "boardgamedesigner":
				stats.designers[link.Value]++
			case "boardgamemechanic":
				stats.mechanics[link.Value]++
			case "boardgamecategory":
				stats.categories[link.Value]++
			}
		}

		if g.Ratings.Owned.Value >= stats.mostPopularCount {
			stats.mostPopularName = g.PrimaryName()
			stats.mostPopularCount = g.Ratings.Owned.Value
		}

		if g.Ratings.Owned.Value < stats.leastPopularCount {
			stats.leastPopularName = g.PrimaryName()
			stats.leastPopularCount = g.Ratings.Owned.Value
		}

		if g.Ratings.Average.Value >= stats.highestRatedAvg {
			stats.highestRatedName = g.PrimaryName()
			stats.highestRatedAvg = g.Ratings.Average.Value
			stats.highestRatedVotes = g.Ratings.UsersRated.Value
		}

		if g.Ratings.Average.Value < stats.lowestRatedAvg {
			stats.lowestRatedName = g.PrimaryName()
			stats.lowestRatedAvg = g.Ratings.Average.Value
			stats.lowestRatedVotes = g.Ratings.UsersRated.Value
		}
	}
}

func printStats(username string, stats *customstats) {
	fmt.Println()
	fmt.Printf("Stats for %s's Collection\n", username)
	fmt.Println()
	fmt.Println("Owned Games")
	fmt.Printf("\tMost played:   %s (%d plays by %s)\n", stats.mostPlayedName, stats.mostPlayedCount, username)
	fmt.Printf("\tMost popular:  %s (%d owners)\n", stats.mostPopularName, stats.mostPopularCount)
	fmt.Printf("\tLeast popular: %s (%d owners)\n", stats.leastPopularName, stats.leastPopularCount)
	fmt.Printf("\tHighest rated: %s (%.1f average, %d votes)\n", stats.highestRatedName, stats.highestRatedAvg, stats.highestRatedVotes)
	fmt.Printf("\tLowest rated:  %s (%.1f average, %d votes)\n", stats.lowestRatedName, stats.lowestRatedAvg, stats.lowestRatedVotes)

	fmt.Println()
	printCollectionStats(10, &stats.designers, "Designers")
	fmt.Println()
	printCollectionStats(10, &stats.mechanics, "Mechanics")
	fmt.Println()
	printCollectionStats(10, &stats.categories, "Categories")
}

func retrieveCollection(username string) (coll *bggo.CollectionResponse) {
	xmldata := httpGetAndReadAll(bggurlcollection + username)
	coll = &bggo.CollectionResponse{}
	unmarshalOrDie(xmldata, coll)
	return
}

func retrieveAndPrintCollectionStats(username string) {
	collection := retrieveCollection(username)
	games := retrieveGames(collection.JoinObjectIDs())

	stats := makeCustomStats()
	collectStatsOnCollection(stats, collection)
	collectStatsOnGames(stats, games)

	printStats(username, stats)
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
