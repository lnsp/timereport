package main

import (
	"bytes"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"time"
)

const DayFormat = `2006-01-02`
const KVDayFormat = `2006_01_02`

//go:embed index.template.html
var index embed.FS

type context struct {
	Group              string
	Reports            string
	DateStart, DateEnd string
}

func main() {
	tmpl := template.Must(template.ParseFS(index, "*"))

	token := os.Getenv("VALAR_TOKEN")
	group := os.Getenv("GROUP")
	kvProject := os.Getenv("KV_PROJECT")
	kvKey := os.Getenv("KV_KEY")

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Get params
		endDate := time.Now()
		startDate := endDate.Add(-time.Hour * 24 * 7) // Go back two weeks

		// Check if end date has been set
		if r.URL.Query().Has("end") {
			endDate, _ = time.Parse(DayFormat, r.URL.Query().Get("end"))
			endDate = endDate.Add(24*time.Hour - time.Second)
			startDate = endDate.Add(-time.Hour * 24 * 7) // Go back two weeks
		}

		// Check if start date has been set
		if r.URL.Query().Has("start") {
			startDate, _ = time.Parse(DayFormat, r.URL.Query().Get("start"))
		}

		// Read all date values in range from startDate to endDate
		reports := []Report{}
		startDateOnly := startDate.Truncate(24 * time.Hour)
		endDateOnly := endDate.Truncate(24 * time.Hour)
		for d := startDateOnly; !d.After(endDateOnly); d = d.Add(time.Hour * 24) {
			reps, err := FetchData(token, kvProject, kvKey+"_"+d.Format(KVDayFormat))
			if err != nil {
				// Could not find key, just skip data
				continue
			}
			reports = append(reports, reps...)
		}

		// Filter for range between start and end date
		start := sort.Search(len(reports), func(i int) bool {
			return reports[i].Timestamp.After(startDate)
		})
		end := sort.Search(len(reports), func(i int) bool {
			return !reports[i].Timestamp.Before(endDate)
		})

		reportsJson, _ := json.Marshal(reports[start:end])

		if err := tmpl.Execute(w, context{
			Group:     group,
			Reports:   string(reportsJson),
			DateStart: startDate.Format(DayFormat),
			DateEnd:   endDate.Format(DayFormat),
		}); err != nil {
			log.Fatal(err)
		}
	})
	http.ListenAndServe(":8080", nil)
}

type Report struct {
	Series    string            `json:"series"`
	Value     float64           `json:"value"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"ts"`
}

func FetchData(token, project, key string) ([]Report, error) {
	url := fmt.Sprintf("https://kv.valar.dev/%s/%s", project, key)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		// Parse error status
		errstr := struct {
			Error string `json:"error"`
		}{}
		if err := json.Unmarshal(data, &errstr); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf(errstr.Error)
	}

	reader := bytes.NewReader(data)
	reports := []Report{}
	for reader.Len() > 0 {
		var report Report

		// Read timestamp
		var unix int64
		binary.Read(reader, binary.LittleEndian, &unix)
		report.Timestamp = time.Unix(unix, 0)

		// Read value
		binary.Read(reader, binary.LittleEndian, &report.Value)

		// Read series and metadata length
		var seriesLen, metadataLen int64
		binary.Read(reader, binary.LittleEndian, &seriesLen)
		binary.Read(reader, binary.LittleEndian, &metadataLen)

		fmt.Println(seriesLen, metadataLen)

		// Read series data
		seriesBytes := make([]byte, seriesLen)
		reader.Read(seriesBytes)
		report.Series = string(seriesBytes)

		// Read metadata
		report.Metadata = make(map[string]string)
		for i := int64(0); i < metadataLen; i++ {
			var keyLen, valueLen int64
			binary.Read(reader, binary.LittleEndian, &keyLen)
			binary.Read(reader, binary.LittleEndian, &valueLen)

			keyBytes, valueBytes := make([]byte, keyLen), make([]byte, valueLen)
			reader.Read(keyBytes)
			reader.Read(valueBytes)

			report.Metadata[string(keyBytes)] = string(valueBytes)
		}

		// Append to reports
		reports = append(reports, report)
	}
	fmt.Println(reports)

	return reports, nil
}
