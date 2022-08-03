package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const KVDayFormat = `2006_01_02`

type Report struct {
	Series    string            `json:"series"`
	Value     float64           `json:"value"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
}

func main() {
	token := os.Getenv("VALAR_TOKEN")
	kvKey, ok := os.LookupEnv("KV_KEY")
	if !ok {
		kvKey = "timeseries"
	}
	kvProject, ok := os.LookupEnv("KV_PROJECT")
	if !ok {
		kvProject = "lennart"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println("reading payload:", string(raw), err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Println("read payload ", string(raw))
		var payload []Report
		if err := json.Unmarshal(raw, &payload); err != nil {
			log.Println("decoding payload:", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Write([]byte("ok\n"))

		if token != "" {
			for i := range payload {
				// Attempt to write to KV
				key := kvKey + "_" + payload[i].Timestamp.Format(KVDayFormat)
				if err := ReportTemperature(token, kvProject, key, &payload[i]); err != nil {
					log.Println("report temperature:", err)
				}
			}
		}
	})
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

func ReportTemperature(token, project, key string, payload *Report) error {
	url := fmt.Sprintf("https://kv.valar.dev/%s/%s?op=append", project, key)

	buf := new(bytes.Buffer)

	// Write fixed length data first
	binary.Write(buf, binary.LittleEndian, payload.Timestamp.Unix())
	binary.Write(buf, binary.LittleEndian, payload.Value)
	binary.Write(buf, binary.LittleEndian, int64(len(payload.Series)))
	binary.Write(buf, binary.LittleEndian, int64(len(payload.Metadata)))

	// Now do non-fixed length data
	buf.Write([]byte(payload.Series))
	for key, value := range payload.Metadata {
		binary.Write(buf, binary.LittleEndian, int64(len(key)))
		binary.Write(buf, binary.LittleEndian, int64(len(value)))
		buf.Write([]byte(key))
		buf.Write([]byte(value))
	}

	request, err := http.NewRequest(http.MethodPost, url, buf)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	return response.Body.Close()
}
