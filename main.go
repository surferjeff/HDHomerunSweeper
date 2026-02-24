package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

// DiscoveryResult maps the JSON from SiliconDust's discovery API
type DiscoveryResult struct {
	DeviceID   string
	StorageURL string
}

// Recording represents the JSON structure of an HDHomeRun recording
type Recording struct {
	EpisodesURL string
	StartTime   int64
	// ImageURL string
	// PosterURL string, only for movies
	Category string
	Title    string
	SeriesID string
}

type SeriesStat struct {
	Title        string
	Count        uint32
	TotalSize    int64
	EpisodesURLs []string
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Subcommands
	listCommand := flag.NewFlagSet("list", flag.ExitOnError)
	deleteSeriesCommand := flag.NewFlagSet("delete-series", flag.ExitOnError)

	// Flags for subcommands
	deleteSeriesTitle := deleteSeriesCommand.String("title", "", "A unique prefix of the title of the series to delete (required)")
	deleteSeriesForever := deleteSeriesCommand.Bool("forever", false, "Never attempt to rerecord the episodes being deleted.")

	switch os.Args[1] {
	case "list":
		listCommand.Parse(os.Args[2:])
		storageUrl := getStorageUrlOrExit()
		listRecordings(storageUrl)

	case "delete-series":
		deleteSeriesCommand.Parse(os.Args[2:])
		if *deleteSeriesTitle == "" {
			deleteSeriesCommand.PrintDefaults()
			os.Exit(1)
		}
		deleteSeries(*deleteSeriesTitle, *deleteSeriesForever)

	default:
		fmt.Println("Unknown command.")
		printUsage()
		os.Exit(1)
	}
}

func deleteSeries(title string, forever bool) {
	storageUrl := getStorageUrlOrExit()

	recordings, err := fetchRecordings(storageUrl)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	var deleteRecordings []Recording
	for _, recording := range recordings {
		if strings.HasPrefix(recording.Title, title) {
			if len(deleteRecordings) > 0 {
				fmt.Printf("More than one title matches '%s':\n%s\n%s\n",
					title, deleteRecordings[0].Title, recording.Title)
				return
			}
			deleteRecordings = append(deleteRecordings, recording)
		}
	}

	if 0 == len(deleteRecordings) {
		fmt.Printf("Nothing matches '%s'\n", title)
		return
	}

	seriesMap := collectRecordings(recordings)

	oneMap := make(map[string]*SeriesStat)
	for key, stat := range seriesMap {
		oneMap[key] = stat
		aggregateStats(stat)
	}

}

func printUsage() {
	fmt.Println("HDHomeRun DVR CLI")
	fmt.Println("Usage:")
	fmt.Println("  hdhr-cli list")
	fmt.Println("  hdhr-cli delete-series --title \"Series Title\"  --forever")
}

// getStorageUrlOrExit wraps the discovery logic and terminates if it fails
func getStorageUrlOrExit() string {
	ip, err := getStorageUrl()
	if err != nil {
		fmt.Printf("Discovery Error: %v\n", err)
		os.Exit(1)
	}
	return ip
}

// getStorageUrl queries the SiliconDust cloud API to find the local IP
func getStorageUrl() (string, error) {
	fmt.Println("Searching for HDHomeRun devices on the local network...")

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://hdhomerun.local/discover.json")
	if err != nil {
		return "", fmt.Errorf("failed to query discovery API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var discovery DiscoveryResult
	if err := json.Unmarshal(body, &discovery); err != nil {
		return "", fmt.Errorf("failed to parse discovery JSON: %w", err)
	}

	return discovery.StorageURL, nil
}

type Episode struct {
	PlayURL string
	CmdURL  string
}

// fetchRecordings gets the JSON array of recordings from the device
func fetchRecordings(recordingsUrl string) ([]Recording, error) {
	resp, err := http.Get(recordingsUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HDHomeRun DVR engine: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var recordings []Recording
	if err := json.Unmarshal(body, &recordings); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return recordings, nil
}

func listRecordings(storageUrl string) {
	recordings, err := fetchRecordings(storageUrl)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	seriesMap := collectRecordings(recordings)

	oneMap := make(map[string]*SeriesStat)
	for key, stat := range seriesMap {
		oneMap[key] = stat
		aggregateStats(stat)
	}
	printSeriesMap(oneMap)
}

func printSeriesMap(seriesMap map[string]*SeriesStat) {
	// 1. Convert the map values into a slice so we can sort them
	stats := make([]*SeriesStat, 0, len(seriesMap))
	for _, stat := range seriesMap {
		stats = append(stats, stat)
	}

	// 2. Sort the slice by TotalSize in descending order
	slices.SortFunc(stats, func(a, b *SeriesStat) int {
		if a.TotalSize > b.TotalSize {
			return 1
		} else if a.TotalSize < b.TotalSize {
			return -1
		}
		return 0
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SERIES TITLE\tEPISODES\tSTORAGE USED\t")
	fmt.Fprintln(w, "------------\t--------\t------------\t")

	// 3. Iterate over the sorted slice instead of the map
	for _, stat := range stats {
		sizeGB := float64(stat.TotalSize) / (1024 * 1024 * 1024)
		fmt.Fprintf(w, "%s\t%d\t%.2f GB\t\n", stat.Title, stat.Count, sizeGB)
	}
	w.Flush()

	fmt.Printf("\nTotal Series Found: %d\n", len(seriesMap))
}

func collectRecordings(recordings []Recording) map[string]*SeriesStat {
	seriesMap := make(map[string]*SeriesStat)
	for _, rec := range recordings {
		series, exists := seriesMap[rec.SeriesID]
		if !exists {
			series = &SeriesStat{
				Title: rec.Title,
			}
			seriesMap[rec.SeriesID] = series
		}

		series.Count++
		series.EpisodesURLs = append(series.EpisodesURLs, rec.EpisodesURL)
	}
	return seriesMap
}

func aggregateStats(stat *SeriesStat) error {
	stat.Count = 0
	stat.TotalSize = 0
	for _, url := range stat.EpisodesURLs {
		episodes, err := getEpisodes(url)
		if err != nil {
			return err
		}

		for _, episode := range episodes {
			size, err := getEpisodeSize(episode.PlayURL)
			if err != nil {
				return err
			}
			stat.Count += 1
			stat.TotalSize += size
		}
	}
	return nil
}

func getEpisodes(url string) ([]Episode, error) {
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch %v: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%v returned status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var episodes []Episode
	if err := json.Unmarshal(body, &episodes); err != nil {
		return nil, fmt.Errorf("failed to parse episode JSON: %w", err)
	}
	return episodes, nil
}

func getEpisodeSize(playUrl string) (int64, error) {
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(playUrl)
	if err != nil {
		return 0, fmt.Errorf("Failed to fetch %v: %w", playUrl, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%v returned status %d", playUrl, resp.StatusCode)
	}

	return resp.ContentLength, nil
}

/*
HTTP	544	POST /recorded/cmd?id=e2a5d6b199c2be7f&cmd=delete&rerecord=0 HTTP/1.1
HTTP	544	POST /recorded/cmd?id=fd3c41514da2c1a2&cmd=delete&rerecord=1 HTTP/1.1
*/
